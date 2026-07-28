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

func Creators(entries []Entry, sourcePath func(string) string) ([]ipgeo.SourceCreator, error) {
	creators := make([]ipgeo.SourceCreator, 0, len(entries))
	for _, entry := range entries {
		creator, err := Creator(entry, sourcePath)
		if err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	return creators, nil
}

func Creator(entry Entry, sourcePath func(string) string) (ipgeo.SourceCreator, error) {
	path := sourcePath(entry.Filename)
	companionPath := ""
	if entry.CompanionFilename != "" {
		companionPath = sourcePath(entry.CompanionFilename)
	}
	var creator ipgeo.SourceCreator
	switch entry.Type {
	case "mmdb":
		creator = ipgeo.MMDB(entry.Name, path, companionPath)
	case "ipdb":
		creator = ipgeo.IPDB(entry.Name, path)
	case "xdb":
		creator = ipgeo.XDB(entry.Name, path, companionPath)
	case "ip2location":
		creator = ipgeo.IP2Location(entry.Name, path)
	default:
		return ipgeo.SourceCreator{}, fmt.Errorf("configure source %s: unknown source type: %s", entry.Name, entry.Type)
	}
	return creator.
		Decorate(ipgeo.Singleflight()).
		Decorate(ipgeo.Cache(1024, 0, 0)), nil
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
