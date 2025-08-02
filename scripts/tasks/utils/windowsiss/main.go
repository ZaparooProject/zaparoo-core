package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

type InnoSetupData struct {
	Version                string
	OutputVersion          string
	Arch                   string
	ArchitecturesAllowed   string
	ArchitecturesInstall64 string
	Year                   string
}

func main() {
	version := flag.String("version", "", "Version number")
	arch := flag.String("arch", "", "Architecture (386, amd64, or arm64)")
	flag.Parse()

	if *version == "" || *arch == "" {
		_, _ = fmt.Fprintf(os.Stderr, "Error: version and arch are required\n")
		os.Exit(1)
	}

	outputVersion := *version
	if strings.Contains(*version, "-") {
		*version = "0.0.0"
	}

	var archAllowed, archInstall string
	switch *arch {
	case "amd64":
		archAllowed = "x64compatible"
		archInstall = "x64compatible"
	case "arm64":
		archAllowed = "arm64"
		archInstall = "arm64"
	case "386":
		archAllowed = "x86compatible"
		archInstall = "" // 32-bit doesn't need special install mode
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Error: unsupported architecture: %s\n", *arch)
		os.Exit(1)
	}

	data := InnoSetupData{
		Version:                *version,
		OutputVersion:          outputVersion,
		Arch:                   *arch,
		ArchitecturesAllowed:   archAllowed,
		ArchitecturesInstall64: archInstall,
		Year:                   strconv.Itoa(time.Now().Year()),
	}

	tmpl, err := template.ParseFiles("cmd/windows/setup.iss.tmpl")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error parsing template: %v\n", err)
		os.Exit(1)
	}

	outFile, err := os.Create("_build/windows_" + *arch + "/setup.iss")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer func(outFile *os.File) {
		_ = outFile.Close()
	}(outFile)

	if err := tmpl.Execute(outFile, data); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error executing template: %v\n", err)
		os.Exit(1)
	}
}
