package main

import (
	"os/exec"
	"strings"
)

func checkoutRevision(source, revision string) error {
	head, err := exec.Command("git", "-C", source, "rev-parse", "--verify", "HEAD").Output()
	if err != nil {
		// A newly initialized cache has no commit. An unsuccessful rev-parse
		// can print "HEAD", which must never be treated as an existing revision.
		head = nil
	}
	if strings.TrimSpace(string(head)) == revision {
		return nil
	}
	if err = run(source, nil, "git", "fetch", "--depth", "1", "origin", revision); err != nil {
		return err
	}
	if len(head) != 0 {
		// Retire generated desktop patches before switching upstream revisions.
		if err = run(source, nil, "git", "restore", "--source=HEAD", "--", "main.go", "config/config.go", "plugin/plugin.go", "service/search_service.go"); err != nil {
			return err
		}
	}
	return run(source, nil, "git", "checkout", "--detach", revision)
}
