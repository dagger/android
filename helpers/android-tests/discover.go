package main

// The discover command lists JVM test classes from Java and Kotlin sources
// without Gradle, a JDK, or project dependencies, so listing checks never runs
// the build. It follows the test frameworks' own rules where the source shows
// them: a concrete class is a test class when it, a superclass, or an
// interface in the scanned sources declares tests (@Test, @ParameterizedTest,
// @RepeatedTest, @TestFactory, @TestTemplate, @Theory), uses a JUnit 4
// @RunWith runner, or extends JUnit 3's TestCase. Nested classes count when
// JUnit runs them on their own: @Nested, Java static, or Kotlin non-inner.
//
// Gradle remains the authority. A listed class Gradle does not run fails its
// check as not run; an unlisted class's failure fails every check sharing the
// run.

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

var testAnnotations = map[string]bool{
	"Test": true, "ParameterizedTest": true, "RepeatedTest": true,
	"TestFactory": true, "TestTemplate": true, "Theory": true,
}

type classDecl struct {
	pkg, sourceSet string
	// binary is the class's binary name within its package, Outer$Inner.
	binary   string
	name     string
	kotlin   bool
	concrete bool // not abstract, not an interface, enum, annotation or object
	nested   bool
	runnable bool // top-level, or a nested class JUnit runs on its own
	ownTests bool
	supers   []string
}

// tokenize strips comments and literals and splits source into identifiers
// and single-character punctuation. Backticked Kotlin names are identifiers.
func tokenize(src string) []string {
	var tokens []string
	r := []rune(src)
	isIdent := func(c rune) bool { return c == '_' || c == '$' || unicode.IsLetter(c) || unicode.IsDigit(c) }
	for i := 0; i < len(r); {
		c := r[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '/' && i+1 < len(r) && r[i+1] == '/':
			for i < len(r) && r[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(r) && r[i+1] == '*':
			i += 2
			for i+1 < len(r) && !(r[i] == '*' && r[i+1] == '/') {
				i++
			}
			i += 2
		case c == '"' && i+2 < len(r) && r[i+1] == '"' && r[i+2] == '"':
			// Kotlin raw strings and Java text blocks.
			i += 3
			for i+2 < len(r) && !(r[i] == '"' && r[i+1] == '"' && r[i+2] == '"') {
				i++
			}
			i += 3
			tokens = append(tokens, `""`)
		case c == '"' || c == '\'':
			i++
			for i < len(r) && r[i] != c && r[i] != '\n' {
				if r[i] == '\\' {
					i++
				}
				i++
			}
			i++
			tokens = append(tokens, `""`)
		case c == '`':
			j := i + 1
			for j < len(r) && r[j] != '`' && r[j] != '\n' {
				j++
			}
			tokens = append(tokens, string(r[i+1:min(j, len(r))]))
			i = j + 1
		case isIdent(c):
			j := i
			for j < len(r) && isIdent(r[j]) {
				j++
			}
			tokens = append(tokens, string(r[i:j]))
			i = j
		default:
			tokens = append(tokens, string(c))
			i++
		}
	}
	return tokens
}

func isIdentifier(s string) bool {
	if s == "" || s == `""` {
		return false
	}
	c := []rune(s)[0]
	return c == '_' || c == '$' || unicode.IsLetter(c)
}

var declarationKeywords = map[string]bool{"class": true, "interface": true, "enum": true, "record": true, "object": true}

// parseFile finds the classes declared in one source file. It is tolerant:
// source it cannot follow yields fewer classes, never an error.
func parseFile(src, sourceSet string, kotlin bool) []*classDecl {
	t := tokenize(src)
	at := func(i int) string {
		if i >= 0 && i < len(t) {
			return t[i]
		}
		return ""
	}
	pkg := ""
	var classes []*classDecl
	type frame struct {
		class *classDecl
		depth int
	}
	var stack []frame
	depth := 0
	// Modifiers and annotations since the last statement boundary.
	var annotations []string
	modifiers := map[string]bool{}
	reset := func() {
		annotations = nil
		modifiers = map[string]bool{}
	}
	current := func() *classDecl {
		if len(stack) == 0 {
			return nil
		}
		return stack[len(stack)-1].class
	}
	for i := 0; i < len(t); i++ {
		tok := t[i]
		switch {
		case tok == "package" && depth == 0 && pkg == "":
			// Kotlin has no semicolon: the name ends at the first token that
			// does not continue a dotted identifier.
			var parts []string
			for i+1 < len(t) {
				next := t[i+1]
				if next == "." {
					i++
					continue
				}
				if !isIdentifier(next) || (len(parts) > 0 && t[i] != ".") {
					break
				}
				parts = append(parts, next)
				i++
			}
			pkg = strings.Join(parts, ".")
		case tok == "@" && at(i+1) != "interface":
			// Qualified annotation names keep their last segment.
			name := at(i + 1)
			i++
			for at(i+1) == "." && at(i+2) != "" {
				i += 2
				name = t[i]
			}
			if c := current(); c != nil && testAnnotations[name] {
				c.ownTests = true
			}
			annotations = append(annotations, name)
			// Skip the annotation's arguments, which may name classes.
			if at(i+1) == "(" {
				parens := 0
				for i++; i < len(t); i++ {
					if t[i] == "(" {
						parens++
					} else if t[i] == ")" {
						parens--
						if parens == 0 {
							break
						}
					}
				}
			}
		case declarationKeywords[tok] || (tok == "@" && at(i+1) == "interface"):
			prev := at(i - 1)
			if prev == "." || prev == ":" || prev == "::" {
				continue // Foo.class, Foo::class
			}
			if tok == "@" {
				i++
				modifiers["annotation"] = true
				tok = "interface"
			}
			name := at(i + 1)
			if !isIdentifier(name) || name == "class" {
				// Anonymous objects and "enum class"/"companion object" prefixes.
				if tok == "enum" || tok == "annotation" {
					modifiers[tok] = true
				}
				if name == "class" {
					continue
				}
				reset()
				continue
			}
			i++
			outer := current()
			c := &classDecl{pkg: pkg, sourceSet: sourceSet, name: name, kotlin: kotlin}
			if outer != nil {
				c.binary = outer.binary + "$" + name
				c.nested = true
			} else {
				c.binary = name
			}
			c.concrete = tok == "class" || tok == "record"
			c.concrete = c.concrete && !modifiers["abstract"] && !modifiers["enum"] && !modifiers["annotation"] && !modifiers["sealed"]
			nested := false
			for _, a := range annotations {
				if a == "RunWith" {
					c.ownTests = true
				}
				if a == "Nested" {
					nested = true
				}
			}
			switch {
			case outer == nil:
				c.runnable = true
			case nested:
				c.runnable = true
			case kotlin:
				c.runnable = !modifiers["inner"]
			default:
				c.runnable = modifiers["static"]
			}
			// The header: supertypes up to the body, or a Kotlin class without one.
			parens, angles, inSupers := 0, 0, false
			body := false
		header:
			for i+1 < len(t) {
				i++
				h := t[i]
				switch h {
				case "(":
					parens++
				case ")":
					parens--
				case "<":
					angles++
				case ">":
					angles--
				}
				if parens > 0 || angles > 0 {
					continue
				}
				switch {
				case h == "{":
					body = true
					break header
				case h == ";" || h == "}":
					i--
					break header
				case kotlin && (h == "fun" || h == "val" || h == "var" || h == "@" || declarationKeywords[h]):
					i--
					break header
				case h == "extends" || h == "implements" || h == ":":
					inSupers = true
				case h == "where" || h == "permits" || h == "by":
					inSupers = false
				case inSupers && h != "," && h != "." && h != ")" && h != ">" && at(i+1) != ".":
					c.supers = append(c.supers, h)
				}
			}
			classes = append(classes, c)
			reset()
			if body {
				depth++
				stack = append(stack, frame{c, depth})
			}
		case tok == "{":
			depth++
			reset()
		case tok == "}":
			if len(stack) > 0 && stack[len(stack)-1].depth == depth {
				stack = stack[:len(stack)-1]
			}
			depth--
			reset()
		case tok == ";":
			reset()
		default:
			modifiers[tok] = true
		}
	}
	return classes
}

// testClasses resolves inheritance by simple name across all scanned sources.
func testClasses(classes []*classDecl) []*classDecl {
	byName := map[string][]*classDecl{}
	for _, c := range classes {
		byName[c.name] = append(byName[c.name], c)
	}
	memo := map[*classDecl]bool{}
	visiting := map[*classDecl]bool{}
	var hasTests func(c *classDecl) bool
	hasTests = func(c *classDecl) bool {
		if v, ok := memo[c]; ok {
			return v
		}
		if visiting[c] {
			return false
		}
		visiting[c] = true
		result := c.ownTests
		for _, s := range c.supers {
			if s == "TestCase" {
				result = true
			}
			for _, super := range byName[s] {
				result = result || hasTests(super)
			}
		}
		visiting[c] = false
		memo[c] = result
		return result
	}
	var tests []*classDecl
	for _, c := range classes {
		if c.concrete && c.runnable && hasTests(c) {
			tests = append(tests, c)
		}
	}
	return tests
}

func (c *classDecl) qualified() string {
	if c.pkg == "" {
		return c.binary
	}
	return c.pkg + "." + c.binary
}

// discover walks <root>/<sourceSet>/{java,kotlin}/**.{java,kt} for source sets
// whose name starts with "test", and prints "<fqcn>\t<sourceSet>" lines in
// byte order. The caller keeps the source sets of its variant.
func discover(args []string) error {
	flags := flag.NewFlagSet("discover", flag.ExitOnError)
	root := flags.String("root", "", "the Gradle module's src directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var classes []*classDecl
	err := filepath.WalkDir(*root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == *root {
				return filepath.SkipAll // a module without test sources
			}
			return err
		}
		kotlin := strings.HasSuffix(path, ".kt")
		if d.IsDir() || !(kotlin || strings.HasSuffix(path, ".java")) {
			return nil
		}
		rel, err := filepath.Rel(*root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) < 3 || !strings.HasPrefix(parts[0], "test") || (parts[1] != "java" && parts[1] != "kotlin") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read test source %s: %w", rel, err)
		}
		classes = append(classes, parseFile(string(src), parts[0], kotlin)...)
		return nil
	})
	if err != nil {
		return err
	}
	var lines []string
	seen := map[string]bool{}
	for _, c := range testClasses(classes) {
		line := c.qualified() + "\t" + c.sourceSet
		if !seen[line] {
			seen[line] = true
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	out := bufio.NewWriter(os.Stdout)
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
	return out.Flush()
}
