// Copyright 2018 Red Hat, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rpmostree

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/coreos/mantle/kola/cluster"
	"github.com/coreos/mantle/kola/register"
	"github.com/coreos/mantle/platform"
)

func init() {
	register.Register(&register.Test{
		Run:         rpmOstreeStatus,
		ClusterSize: 1,
		Name:        "rhcos.rpmostree.status",
		Distros:     []string{"rhcos"},
	})
}

var (
	// hard code the osname for RHCOS
	// TODO: should this also support FCOS?
	rhcosOsname string = "rhcos"

	// Regex to extract version number from "rpm-ostree status"
	rpmOstreeVersionRegex string = `^Version: (\d+\.\d+\.\d+).*`
)

// rpmOstreeDeployment represents some of the data of an rpm-ostree deployment
type rpmOstreeDeployment struct {
	Booted            bool     `json:"booted"`
	Checksum          string   `json:"checksum"`
	Origin            string   `json:"origin"`
	Osname            string   `json:"osname"`
	Packages          []string `json:"packages"`
	RequestedPackages []string `json:"requested-packages"`
	Version           string   `json:"version"`
}

// simplifiedRpmOstreeStatus contains deployments from rpm-ostree status
type simplifiedRpmOstreeStatus struct {
	Deployments []rpmOstreeDeployment
}

// getRpmOstreeStatusJSON returns an unmarshal'ed JSON object that contains
// a limited representation of the output of `rpm-ostree status --json`
func getRpmOstreeStatusJSON(c cluster.TestCluster, m platform.Machine) (simplifiedRpmOstreeStatus, error) {
	target := simplifiedRpmOstreeStatus{}
	rpmOstreeJSON, err := c.SSH(m, "rpm-ostree status --json")
	if err != nil {
		return target, fmt.Errorf("Could not get rpm-ostree status: %v", err)
	}

	err = json.Unmarshal(rpmOstreeJSON, &target)
	if err != nil {
		return target, fmt.Errorf("Couldn't umarshal the rpm-ostree status JSON data: %v", err)
	}

	return target, nil
}

// rpmOstreeCleanup calls 'rpm-ostree cleanup -rpmb' on a host and verifies
// that only one deployment remains
func rpmOstreeCleanup(c cluster.TestCluster, m platform.Machine) error {
	c.MustSSH(m, "sudo rpm-ostree cleanup -rpmb")

	// one last check to make sure we are back to the original state
	cleanupStatus, err := getRpmOstreeStatusJSON(c, m)
	if err != nil {
		return fmt.Errorf(`Failed to get status JSON: %v`, err)
	}

	if len(cleanupStatus.Deployments) != 1 {
		return fmt.Errorf(`Cleanup left more than one deployment`)
	}
	return nil
}

// rpmOstreeStatus does some sanity checks on the output from
// `rpm-ostree status` and `rpm-ostree status --json`
func rpmOstreeStatus(c cluster.TestCluster) {
	m := c.Machines()[0]

	// check that rpm-ostreed is static?
	enabledOut := c.MustSSH(m, "systemctl is-enabled rpm-ostreed")
	if string(enabledOut) != "static" {
		c.Fatalf(`The "rpm-ostreed" service is not "static": got %v`, string(enabledOut))
	}

	status, err := getRpmOstreeStatusJSON(c, m)
	if err != nil {
		c.Fatal(err)
	}

	// after running an 'rpm-ostree' command the daemon should be active
	statusOut := c.MustSSH(m, "systemctl is-active rpm-ostreed")
	if string(statusOut) != "active" {
		c.Fatalf(`The "rpm-ostreed" service is not active: got %v`, string(statusOut))
	}

	// should only have one deployment
	if len(status.Deployments) != 1 {
		c.Fatalf("Expected one deployment; found %d deployments", len(status.Deployments))
	}

	// the osname should only be RHCOS
	// TODO: perhaps this should also support FCOS?
	if status.Deployments[0].Osname != rhcosOsname {
		c.Fatalf(`"osname" has incorrect value: want %q, got %q`, rhcosOsname, status.Deployments[0].Osname)
	}

	// deployment should be booted (duh!)
	if !status.Deployments[0].Booted {
		c.Fatalf(`Deployment does not report as being booted`)
	}

	// let's validate that the version from the JSON matches the normal output
	var rpmOstreeVersion string
	rpmOstreeStatusOut := c.MustSSH(m, "rpm-ostree status")
	reVersion, err := regexp.Compile(rpmOstreeVersionRegex)
	statusArray := strings.Split(string(rpmOstreeStatusOut), "\n")
	for _, line := range statusArray {
		versionMatch := reVersion.FindStringSubmatch(strings.Trim(line, " "))
		if versionMatch != nil {
			// versionMatch should be like `[Version: 4.0.5516 (2018-09-12 17:22:06) 4.0.5516]`
			// i.e. the full match and the group we want
			// `versionMatch[len(versionMatch)-1]` gets the last element in the array
			rpmOstreeVersion = versionMatch[len(versionMatch)-1]
		}
	}

	if rpmOstreeVersion == "" {
		c.Fatalf(`Unable to determine version from "rpm-ostree status"`)
	}

	if rpmOstreeVersion != status.Deployments[0].Version {
		c.Fatalf(`The version numbers did not match -> from JSON: %q; from stdout: %q`, status.Deployments[0].Version, rpmOstreeVersion)

	}
}
