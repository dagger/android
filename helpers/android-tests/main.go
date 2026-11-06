// android-tests discovers JVM test classes from source and indexes the events
// of a shared Gradle test run into one bounded result per selected class.
package main

import (
	"fmt"
	"os"
)

func main() {
	var err error
	switch command := append(os.Args[1:], "")[0]; command {
	case "discover":
		err = discover(os.Args[2:])
	case "index":
		err = index(os.Args[2:])
	default:
		err = fmt.Errorf("usage: android-tests discover|index [flags]; got %q", command)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
