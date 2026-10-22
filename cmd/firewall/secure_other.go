//go:build !linux

package main

import "fmt"

func secureBinary(string) error { return fmt.Errorf("native firewall operations require Linux") }
func securePaths(string, string) error {
	return fmt.Errorf("native firewall operations require Linux; preview is available on this platform")
}
