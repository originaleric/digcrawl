package config

import (
	"bytes"
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

// MigrateBrowserRuntimeProfile changes only the obsolete executable override
// and background visibility setting. Cookie stores, domains, proxies and all
// unrelated effective values are preserved. Callers stop old owners first.
func MigrateBrowserRuntimeProfile(ctx context.Context, path string) error {
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	unlock, err := lockSkillProfile(ctx, actual)
	if err != nil {
		return err
	}
	defer unlock()
	data, err := os.ReadFile(actual)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err = yaml.Unmarshal(data, &document); err != nil {
		return err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("browser migration requires a YAML mapping")
	}
	root := document.Content[0]
	var original map[string]interface{}
	if err := root.Decode(&original); err != nil {
		return err
	}
	// Effective values are the source of truth, regardless of whether the
	// mapping was written directly, aliased or inherited through root merges.
	expectedLocal := map[string]interface{}{}
	if value := original["LocalBrowser"]; value != nil {
		var ok bool
		expectedLocal, ok = value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("LocalBrowser must be a mapping")
		}
	}
	expectedLocal["ChromePath"], expectedLocal["Headless"] = "", true
	var desired yaml.Node
	if err := desired.Encode(expectedLocal); err != nil {
		return err
	}
	oldLocal := yamlMappingValue(root, "LocalBrowser", false)
	copyBrowserMigrationComments(&desired, oldLocal)
	// All definitions in the replaced subtree can have external consumers.
	if err := detachBrowserMigrationAliases(root); err != nil {
		return err
	}
	replaced := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "LocalBrowser" {
			root.Content[i+1] = &desired
			replaced = true
			break
		}
	}
	if !replaced {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "LocalBrowser"}, &desired)
	}
	original["LocalBrowser"] = expectedLocal
	updated, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	var verified struct {
		LocalBrowser LocalBrowserConfig `yaml:"LocalBrowser"`
	}
	if err := yaml.Unmarshal(updated, &verified); err != nil {
		return err
	}
	if verified.LocalBrowser.ChromePath != "" || verified.LocalBrowser.Headless == nil || !*verified.LocalBrowser.Headless {
		return fmt.Errorf("browser migration did not produce the required effective configuration")
	}
	var effective map[string]interface{}
	if err := yaml.Unmarshal(updated, &effective); err != nil {
		return err
	}
	expectedValues, err := yaml.Marshal(original)
	if err != nil {
		return err
	}
	actualValues, err := yaml.Marshal(effective)
	if err != nil {
		return err
	}
	if !bytes.Equal(expectedValues, actualValues) {
		return fmt.Errorf("browser migration changed unrelated effective configuration")
	}
	backup := actual + ".before-isolated-browser"
	if info, statErr := os.Lstat(backup); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("browser migration backup is not a regular private file")
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	} else {
		f, err := os.CreateTemp(filepath.Dir(actual), ".browser-migration-backup-")
		if err != nil {
			return err
		}
		defer os.Remove(f.Name())
		if err = f.Chmod(0600); err != nil {
			f.Close()
			return err
		}
		_, writeErr := f.Write(data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		// Publish a complete backup without replacing an existing snapshot.
		if err = os.Link(f.Name(), backup); err != nil {
			return err
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return writeConfigAtomically(actual, updated)
}

// Detach aliases whose definitions will disappear with the selected subtree.
// The Alias pointers are read, not followed while walking Content (which can
// otherwise turn reference cycles into unbounded recursion).
func detachBrowserMigrationAliases(root *yaml.Node) error {
	local := yamlMappingValue(root, "LocalBrowser", false)
	if local == nil {
		return nil
	}
	affected := map[*yaml.Node]bool{}
	var mark func(*yaml.Node)
	mark = func(node *yaml.Node) {
		affected[node] = true
		for _, child := range node.Content {
			mark(child)
		}
	}
	mark(local)
	var visit func(*yaml.Node) error
	visit = func(node *yaml.Node) error {
		if node.Kind == yaml.AliasNode && affected[node.Alias] {
			var value interface{}
			if err := node.Decode(&value); err != nil {
				return err
			}
			var replacement yaml.Node
			if err := replacement.Encode(value); err != nil {
				return err
			}
			copyBrowserMigrationComments(&replacement, node)
			*node = replacement
			return nil
		}
		for _, child := range node.Content {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root)
}

// Formatting stays in this browser-specific migration; values never come
// from this traversal. Unmatched merge comments are retained on the mapping.
func copyBrowserMigrationComments(target, source *yaml.Node) {
	if source == nil {
		return
	}
	target.HeadComment, target.LineComment, target.FootComment = source.HeadComment, source.LineComment, source.FootComment
	if target.Kind == yaml.MappingNode || target.Kind == yaml.SequenceNode {
		target.HeadComment = strings.TrimSpace(target.HeadComment + "\n" + target.LineComment)
		target.LineComment = ""
	}
	if target.Kind == yaml.MappingNode && source.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(source.Content); i += 2 {
			key, value := source.Content[i], source.Content[i+1]
			found := false
			for j := 0; j+1 < len(target.Content); j += 2 {
				if target.Content[j].Value == key.Value {
					copyBrowserMigrationComments(target.Content[j], key)
					copyBrowserMigrationComments(target.Content[j+1], value)
					found = true
					break
				}
			}
			if !found {
				for _, node := range []*yaml.Node{key, value} {
					target.HeadComment = strings.TrimSpace(target.HeadComment + "\n" + node.HeadComment + "\n" + node.LineComment + "\n" + node.FootComment)
				}
			}
		}
	} else if target.Kind == yaml.SequenceNode && source.Kind == yaml.SequenceNode {
		for i, node := range source.Content {
			if i < len(target.Content) {
				copyBrowserMigrationComments(target.Content[i], node)
			}
		}
	}
}
