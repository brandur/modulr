package myaml

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"

	"github.com/brandur/modulr/context"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

func ParseFile(c *context.Context, source string, data interface{}) error {
	raw, err := ioutil.ReadFile(source)
	if err != nil {
		return errors.Wrap(err, "Error reading file")
	}

	err = yaml.Unmarshal(raw, data)
	if err != nil {
		return errors.Wrap(err, "Error unmarshaling YAML")
	}

	c.Log.Debugf("myaml: Parsed file: %s", source)
	return nil
}

func ParseFileFrontmatter(c *context.Context, source string, data interface{}) ([]byte, error) {
	raw, err := ioutil.ReadFile(source)
	if err != nil {
		return nil, errors.Wrap(err, "Error reading file")
	}

	frontmatter, content, err := splitFrontmatter(string(raw))
	if err != nil {
		return nil, errors.Wrap(err, "Error splitting frontmatter")
	}

	err = yaml.Unmarshal([]byte(frontmatter), data)
	if err != nil {
		return nil, errors.Wrap(err, "Error unmarshaling YAML frontmatter")
	}

	c.Log.Debugf("myaml: Parsed file frontmatter: %s", source)
	return []byte(content), nil
}

// TODO: get rid of this
func ParseFileFrontmatter2(c *context.Context, source string, data interface{}) ([]byte, bool, error) {
	changed := c.Changed(source)
	if !changed && !c.Forced() {
		return nil, changed, nil
	}

	raw, err := ioutil.ReadFile(source)
	if err != nil {
		return nil, changed, errors.Wrap(err, "Error reading file")
	}

	frontmatter, content, err := splitFrontmatter(string(raw))
	if err != nil {
		return nil, changed, errors.Wrap(err, "Error splitting frontmatter")
	}

	err = yaml.Unmarshal([]byte(frontmatter), data)
	if err != nil {
		return nil, changed, errors.Wrap(err, "Error unmarshaling YAML frontmatter")
	}

	c.Log.Debugf("myaml: Parsed file frontmatter: %s", source)
	return []byte(content), changed, nil
}

//
// Private
//

var errBadFrontmatter = fmt.Errorf("Unable to split YAML frontmatter")

var splitFrontmatterRE = regexp.MustCompile("(?m)^---")

func splitFrontmatter(content string) (string, string, error) {
	parts := splitFrontmatterRE.Split(content, 3)

	if len(parts) > 1 && parts[0] != "" {
		return "", "", errBadFrontmatter
	} else if len(parts) == 2 {
		return "", strings.TrimSpace(parts[1]), nil
	} else if len(parts) == 3 {
		return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), nil
	}

	return "", strings.TrimSpace(parts[0]), nil
}
