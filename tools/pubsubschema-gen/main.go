package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(argv []string) error {
	fs := flag.NewFlagSet("pubsubschema-gen", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	pubsubDir := fs.String("pubsub-dir", "gen/proto/infra/pubsub", "Directory containing `*.pubsub.proto` files.")
	globPattern := fs.String("glob", "*.pubsub.proto", "Glob pattern within --pubsub-dir to match pubsub proto files.")
	outputDir := fs.String("output-dir", "", "Directory to write generated schema YAMLs into.")

	if err := fs.Parse(argv); err != nil {
		return err
	}
	if *outputDir == "" {
		return usage(fs, "missing required flag: --output-dir")
	}

	files, err := resolveInputs(*pubsubDir, *globPattern)
	if err != nil {
		return err
	}
	return generateAll(files, *outputDir)
}

func usage(fs *flag.FlagSet, extra string) error {
	var b strings.Builder
	if extra != "" {
		b.WriteString(extra)
		b.WriteString("\n\n")
	}
	b.WriteString("Usage:\n")
	b.WriteString("  pubsubschema-gen [--pubsub-dir DIR] [--glob GLOB] --output-dir DIR\n\n")
	b.WriteString("Flags:\n")
	fs.PrintDefaults()
	return errors.New(b.String())
}

func resolveInputs(pubsubDir, globPattern string) ([]string, error) {
	pattern := filepath.Join(pubsubDir, globPattern)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, m := range matches {
		st, err := os.Stat(m)
		if err != nil {
			continue
		}
		if st.Mode().IsRegular() {
			files = append(files, m)
		}
	}
	sort.Strings(files)
	return files, nil
}

func generateAll(pubsubFiles []string, outputDir string) error {
	if len(pubsubFiles) == 0 {
		return errors.New("no pubsub proto files found")
	}

	// Remove stale generated schema files so kustomize doesn't keep applying old schemas.
	if err := removeGeneratedSchemas(outputDir); err != nil {
		return err
	}

	var generated []string
	for _, p := range pubsubFiles {
		proto, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		name := deriveSchemaNameFromFilename(p)
		out := filepath.Join(outputDir, name+".schema.yaml")
		manifest := schemaManifest(name, normalizeNewlines(string(proto)))
		if err := writeFile(out, manifest); err != nil {
			return err
		}
		fmt.Printf("Wrote %s -> %s\n", name, out)
		generated = append(generated, filepath.Base(out))
	}

	sort.Strings(generated)
	if err := writeKustomization(outputDir, generated); err != nil {
		return err
	}
	return nil
}

func normalizeNewlines(s string) string {
	// Ensure trailing newline for cleaner yaml literal blocks.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimRight(s, "\n") + "\n"
	return s
}

func indentForYAMLLiteralBlock(s string, indent string) string {
	lines := strings.Split(s, "\n")
	// Split keeps last empty element after trailing newline; we want to keep it to
	// preserve the trailing newline but still indent it as a blank line.
	for i, line := range lines {
		if line == "" {
			lines[i] = indent
		} else {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func schemaManifest(schemaName, protoDefinition string) string {
	return "" +
		"apiVersion: pubsub.cnrm.cloud.google.com/v1beta1\n" +
		"kind: PubSubSchema\n" +
		"metadata:\n" +
		"  name: " + schemaName + "\n" +
		"spec:\n" +
		"  type: PROTOCOL_BUFFER\n" +
		"  definition: |\n" +
		indentForYAMLLiteralBlock(protoDefinition, "    ")
}

func writeFile(path string, contents string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Always write with LF endings.
	contents = strings.ReplaceAll(contents, "\r\n", "\n")
	return os.WriteFile(path, []byte(contents), fs.FileMode(0o644))
}

func removeGeneratedSchemas(outputDir string) error {
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".schema.yaml") {
			if err := os.Remove(filepath.Join(outputDir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeKustomization(outputDir string, resources []string) error {
	var b strings.Builder
	b.WriteString("apiVersion: kustomize.config.k8s.io/v1beta1\n")
	b.WriteString("kind: Kustomization\n\n")
	b.WriteString("resources:\n")
	for _, r := range resources {
		b.WriteString("  - ")
		b.WriteString(r)
		b.WriteString("\n")
	}
	return writeFile(filepath.Join(outputDir, "kustomization.yaml"), b.String())
}

func deriveSchemaNameFromFilename(filename string) string {
	// Example: coreapp.test.v1.TestEvent.pubsub.proto -> coreapp-test-v1-testevent
	base := filepath.Base(filename)
	base = strings.TrimSuffix(base, ".pubsub.proto")
	safe := strings.ToLower(base)
	safe = strings.ReplaceAll(safe, ".", "-")
	safe = strings.ReplaceAll(safe, "_", "-")
	return safe
}
