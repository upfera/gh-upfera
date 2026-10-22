package main

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type createOptions struct {
	Name        string
	Description string
	Visibility  string
}

func parseCreateArgs(args []string) (createOptions, error) {
	options := createOptions{Visibility: "private"}
	positionals := make([]string, 0, 1)
	visibilitySet := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			return options, fmt.Errorf("help requested")
		case arg == "--description" || arg == "-d":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return options, fmt.Errorf("%s requires a value", arg)
			}
			i++
			options.Description = args[i]
		case strings.HasPrefix(arg, "--description="):
			options.Description = strings.TrimPrefix(arg, "--description=")
		case arg == "--visibility":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return options, fmt.Errorf("--visibility requires public or private")
			}
			i++
			if err := setVisibility(&options, &visibilitySet, args[i]); err != nil {
				return options, err
			}
		case strings.HasPrefix(arg, "--visibility="):
			if err := setVisibility(&options, &visibilitySet, strings.TrimPrefix(arg, "--visibility=")); err != nil {
				return options, err
			}
		case arg == "--public":
			if err := setVisibility(&options, &visibilitySet, "public"); err != nil {
				return options, err
			}
		case arg == "--private":
			if err := setVisibility(&options, &visibilitySet, "private"); err != nil {
				return options, err
			}
		case strings.HasPrefix(arg, "-"):
			return options, fmt.Errorf("unknown option %q", arg)
		default:
			positionals = append(positionals, arg)
		}
	}

	if len(positionals) != 1 {
		return options, fmt.Errorf("provide exactly one repository name")
	}
	options.Name = positionals[0]
	if err := validateCreateOptions(options); err != nil {
		return options, err
	}
	return options, nil
}

func setVisibility(options *createOptions, visibilitySet *bool, visibility string) error {
	if visibility != "public" && visibility != "private" {
		return fmt.Errorf("visibility must be public or private")
	}
	if *visibilitySet {
		return fmt.Errorf("specify visibility only once")
	}
	options.Visibility = visibility
	*visibilitySet = true
	return nil
}

func validateCreateOptions(options createOptions) error {
	if err := validateRepositoryName(options.Name); err != nil {
		return err
	}
	if err := validateDescription(options.Description); err != nil {
		return err
	}
	if options.Visibility != "public" && options.Visibility != "private" {
		return fmt.Errorf("visibility must be public or private")
	}
	return nil
}

func validateRepositoryName(name string) error {
	if len(name) == 0 || len(name) > 100 {
		return fmt.Errorf("repository name must be between 1 and 100 characters")
	}
	if name == "." || name == ".." || strings.Contains(name, "..") {
		return fmt.Errorf("repository name cannot contain consecutive dots")
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-_.", r)) {
			return fmt.Errorf("repository name %q contains an invalid character", name)
		}
	}
	if strings.HasSuffix(strings.ToLower(name), ".git") {
		return fmt.Errorf("repository name cannot end with .git")
	}
	return nil
}

func validateDescription(description string) error {
	if utf8.RuneCountInString(description) > 350 {
		return fmt.Errorf("description must be 350 characters or fewer")
	}
	for _, r := range description {
		if unicode.IsControl(r) {
			return fmt.Errorf("description cannot contain control characters")
		}
	}
	return nil
}
