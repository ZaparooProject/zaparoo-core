package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"
)

type TemplateData struct {
	Version string
	Year    int
}

func main() {
	version := flag.String("version", "", "Version number")
	flag.Parse()

	if *version == "" {
		_, _ = fmt.Fprint(os.Stderr, "Error: version is required\n")
		os.Exit(1)
	}

	if strings.Contains(*version, "-") {
		*version = "0.0.0"
	}

	data := TemplateData{
		Version: *version,
		Year:    time.Now().Year(),
	}

	tmpl, err := template.ParseFiles("cmd/windows/winres/winres.json.tmpl")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error parsing template: %v\n", err)
		os.Exit(1)
	}

	if err := generateWinresFile(tmpl, data); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error generating winres file: %v\n", err)
		os.Exit(1)
	}
}

func generateWinresFile(tmpl *template.Template, data TemplateData) error {
	outFile, err := os.Create("cmd/windows/winres/winres.json")
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer func(outFile *os.File) {
		_ = outFile.Close()
	}(outFile)

	if err := tmpl.Execute(outFile, data); err != nil {
		return fmt.Errorf("error executing template: %w", err)
	}

	return nil
}
