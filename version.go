package main

import (
	"fmt"
	"runtime"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func versionString() string {
	return fmt.Sprintf("xrouter %s (%s, %s, %s/%s)", version, commit, date, runtime.GOOS, runtime.GOARCH)
}

func printVersion() {
	fmt.Println(versionString())
}
