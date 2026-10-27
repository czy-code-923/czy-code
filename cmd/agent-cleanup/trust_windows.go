package main

import (
	"errors"
	"os/exec"
	"strings"
)

func trustedManifest(path string) error {
	out, e := exec.Command("whoami.exe", "/user").Output()
	if e != nil || !strings.Contains(string(out), "S-1-5-18") {
		return errors.New("cleanup requires SYSTEM; manifest ACL must be SYSTEM/Administrators only")
	}
	return nil
}
