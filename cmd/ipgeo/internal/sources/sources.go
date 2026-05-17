package sources

import (
	"fmt"

	"github.com/kibaamor/ipgeo"
)

var supportedTypes = map[string]struct{}{
	"ip2location": {},
	"ipdb":        {},
	"mmdb":        {},
	"xdb":         {},
}

type Entry struct {
	Name              string   `yaml:"name"`
	Type              string   `yaml:"type"`
	URLs              []string `yaml:"urls"`
	Filename          string   `yaml:"filename"`
	CompanionURLs     []string `yaml:"companion_urls,omitempty"`
	CompanionFilename string   `yaml:"companion_filename,omitempty"`
}

type File struct {
	Name string
	Path string
	URLs []string
}

type Descriptor struct {
	Name      string
	Type      string
	File      string
	Path      string
	Companion *CompanionDescriptor
}

type CompanionDescriptor struct {
	File string
	Path string
}

func IsSupportedType(sourceType string) bool {
	_, ok := supportedTypes[sourceType]
	return ok
}

func Select(entries []Entry, sourceName string) ([]Entry, error) {
	if sourceName == "" {
		return entries, nil
	}
	for _, source := range entries {
		if source.Name == sourceName {
			return []Entry{source}, nil
		}
	}
	return nil, fmt.Errorf("source %q not found; run 'ipgeo info' to list available sources", sourceName)
}

func Options(entries []Entry, sourcePath func(string) string) ([]ipgeo.Option, error) {
	opts := make([]ipgeo.Option, 0, len(entries))
	for _, entry := range entries {
		opt, err := Option(entry, sourcePath)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opt)
	}
	return opts, nil
}

func Option(entry Entry, sourcePath func(string) string) (ipgeo.Option, error) {
	path := sourcePath(entry.Filename)
	companionPath := ""
	if entry.CompanionFilename != "" {
		companionPath = sourcePath(entry.CompanionFilename)
	}
	switch entry.Type {
	case "mmdb":
		return ipgeo.WithMMDB(entry.Name, path, companionPath), nil
	case "ipdb":
		return ipgeo.WithIPDB(entry.Name, path), nil
	case "xdb":
		return ipgeo.WithXDB(entry.Name, path, companionPath), nil
	case "ip2location":
		return ipgeo.WithIP2Location(entry.Name, path), nil
	default:
		return nil, fmt.Errorf("configure source %s: unknown source type: %s", entry.Name, entry.Type)
	}
}

func Files(entries []Entry, sourcePath func(string) string) []File {
	var files []File
	for _, source := range entries {
		files = append(files, File{source.Name, sourcePath(source.Filename), source.URLs})
		if source.CompanionFilename != "" && len(source.CompanionURLs) > 0 {
			files = append(files, File{source.Name + " (companion)", sourcePath(source.CompanionFilename), source.CompanionURLs})
		}
	}
	return files
}

func Describe(entries []Entry, sourcePath func(string) string) []Descriptor {
	descriptors := make([]Descriptor, 0, len(entries))
	for _, source := range entries {
		descriptor := Descriptor{
			Name: source.Name,
			Type: source.Type,
			File: source.Filename,
			Path: sourcePath(source.Filename),
		}
		if source.CompanionFilename != "" {
			descriptor.Companion = &CompanionDescriptor{
				File: source.CompanionFilename,
				Path: sourcePath(source.CompanionFilename),
			}
		}
		descriptors = append(descriptors, descriptor)
	}
	return descriptors
}
