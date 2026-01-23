package main

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"goa.design/goa/v3/codegen"
	"golang.org/x/tools/go/packages"
)

// Generator is the code generation management data structure.
type Generator struct {
	// Command is the name of the command to run.
	Command string

	// DesignPath is the Go import path to the design package.
	DesignPath string

	// Output is the absolute path to the output directory.
	Output string

	// DesignVersion is the major component of the Goa version used by the design DSL.
	// DesignVersion is either 2 or 3.
	DesignVersion int

	// bin is the filename of the generated generator.
	bin string

	// tmpDir is the temporary directory used to compile the generator.
	tmpDir string

	// hasVendorDirectory is a flag to indicate whether the project uses vendoring
	hasVendorDirectory bool
}

// NewGenerator creates a Generator.
func NewGenerator(cmd, path, output string, debug bool) *Generator {
	bin := "goa"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	var version int
	var hasVendorDirectory bool
	{
		version = 2
		matched := false
		startPkgLoad := time.Now()
		pkgs, _ := packages.Load(&packages.Config{Mode: packages.NeedFiles | packages.NeedModule}, path)
		if debug {
			fmt.Fprintf(os.Stderr, "[TIMING]   packages.Load (design files) took %v\n", time.Since(startPkgLoad))
		}
		fset := token.NewFileSet()
		p := regexp.MustCompile(`goa.design/goa/v(\d+)/dsl`)
		for _, pkg := range pkgs {
			if pkg.Module != nil {
				if _, err := os.Stat(filepath.Join(pkg.Module.Dir, "vendor")); !os.IsNotExist(err) {
					hasVendorDirectory = true
				}
			}
			for _, gof := range pkg.GoFiles {
				if bs, err := os.ReadFile(gof); err == nil {
					if f, err := parser.ParseFile(fset, "", string(bs), parser.ImportsOnly); err == nil {
						for _, s := range f.Imports {
							matches := p.FindStringSubmatch(s.Path.Value)
							if len(matches) == 2 {
								matched = true
								version, _ = strconv.Atoi(matches[1]) // We know it's an integer
							}
						}
					}
				}
				if matched {
					break
				}
			}
			if matched {
				break
			}
		}
	}

	return &Generator{
		Command:            cmd,
		DesignPath:         path,
		Output:             output,
		DesignVersion:      version,
		hasVendorDirectory: hasVendorDirectory,
		bin:                bin,
	}
}

// Write writes the main file.
func (g *Generator) Write(_ bool) error {
	// IMPORTANT: Keep the generator temp directory relative to the working
	// directory. The Goa generator uses packages.Load on ".<sep><tmpDir>" and
	// expects a relative path (matching upstream goa cmd behavior).
	tmpDir, err := os.MkdirTemp(".", "goa")
	if err != nil {
		return err
	}
	g.tmpDir = tmpDir

	data := map[string]any{
		"Command":       g.Command,
		"CleanupDirs":   cleanupDirs(g.Command, g.Output),
		"DesignVersion": g.DesignVersion,
	}
	ver := ""
	if g.DesignVersion > 2 {
		ver = "v" + strconv.Itoa(g.DesignVersion) + "/"
	}
	imports := []*codegen.ImportSpec{
		codegen.SimpleImport("flag"),
		codegen.SimpleImport("fmt"),
		codegen.SimpleImport("os"),
		codegen.SimpleImport("path/filepath"),
		codegen.SimpleImport("sort"),
		codegen.SimpleImport("strconv"),
		codegen.SimpleImport("strings"),
		codegen.SimpleImport("time"),
		codegen.SimpleImport("goa.design/goa/" + ver + "codegen"),
		codegen.SimpleImport("goa.design/goa/" + ver + "codegen/generator"),
		codegen.SimpleImport("goa.design/goa/" + ver + "eval"),
		codegen.NewImport("goa", "goa.design/goa/"+ver+"pkg"),
		// Bake the goa-vcr plugin into the generated generator binary.
		codegen.NewImport("_", "github.com/xeger/goa-vcr/plugin/vcr"),
		// Import the design (which registers DSL roots).
		codegen.NewImport("_", g.DesignPath),
	}
	sections := []*codegen.SectionTemplate{
		codegen.Header("Code Generator", "main", imports),
		{
			Name:   "main",
			Source: mainT,
			Data:   data,
		},
	}

	f := &codegen.File{Path: "main.go", SectionTemplates: sections}
	_, err = f.Render(tmpDir)
	return err
}

// Compile compiles the generator.
func (g *Generator) Compile(debug bool) error {
	startLoad := time.Now()
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName}, fmt.Sprintf(".%c%s", filepath.Separator, g.tmpDir))
	if err != nil {
		return err
	}
	if len(pkgs) != 1 {
		return fmt.Errorf("expected to find one package in %s", g.tmpDir)
	}
	if debug {
		fmt.Fprintf(os.Stderr, "[TIMING]   packages.Load (temp dir) took %v\n", time.Since(startLoad))
	}

	if !g.hasVendorDirectory {
		startGet := time.Now()
		if err := g.runGoCmd("get", pkgs[0].PkgPath); err != nil {
			return err
		}
		if debug {
			fmt.Fprintf(os.Stderr, "[TIMING]   go get took %v\n", time.Since(startGet))
		}
	}

	startBuild := time.Now()
	err = g.runGoCmd("build", "-o", g.bin)
	if debug {
		fmt.Fprintf(os.Stderr, "[TIMING]   go build took %v\n", time.Since(startBuild))
	}

	if err != nil && g.hasVendorDirectory {
		if strings.Contains(err.Error(), "cannot find package") && strings.Contains(err.Error(), "/goa.design/goa/v3/codegen/generator") {
			return errors.New("generated code expected `goa.design/goa/v3/codegen/generator` to be present in the vendor directory, see documentation for more details")
		}
	}

	return err
}

// Run runs the compiled binary and return the output lines.
func (g *Generator) Run(debug bool) ([]string, error) {
	args := []string{"--version=" + strconv.Itoa(g.DesignVersion), "--output=" + g.Output, "--cmd=" + " " + strings.Join(os.Args[1:], " "), "--debug=" + strconv.FormatBool(debug)}
	cmd := exec.Command(filepath.Join(g.tmpDir, g.bin), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w\n%s", err, string(out))
	}
	res := strings.Split(string(out), "\n")
	for (len(res) > 0) && (res[len(res)-1] == "") {
		res = res[:len(res)-1]
	}
	return res, nil
}

// Remove deletes the package files.
func (g *Generator) Remove() {
	if g.tmpDir != "" {
		_ = os.RemoveAll(g.tmpDir)
		g.tmpDir = ""
	}
}

func (g *Generator) runGoCmd(args ...string) error {
	gobin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf(`failed to find a go compiler, looked in "%s"`, os.Getenv("PATH"))
	}
	// Ensure module-aware behavior for v3 designs.
	if g.DesignVersion > 2 {
		_ = os.Setenv("GO111MODULE", "on")
	}
	cmd := exec.Command(gobin, args...)
	cmd.Dir = g.tmpDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, string(out))
	}
	return nil
}

func cleanupDirs(cmd, out string) []string {
	out, err := filepath.Abs(out)
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(out, "gen"),
	}
}

// mainT is the template for the generator main.
const mainT = `func main() {
	var (
		out     = flag.String("output", "", "")
		version = flag.String("version", "", "")
		cmdl    = flag.String("cmd", "", "")
		debug   = flag.Bool("debug", false, "")
		ver int
	)
	{
		flag.Parse()
		if *out == "" {
			fail("missing output flag")
		}
		if *version == "" {
			fail("missing version flag")
		}
		if *cmdl == "" {
			fail("missing cmd flag")
		}
		v, err := strconv.Atoi(*version)
		if err != nil {
			fail("invalid version %s", *version)
		}
		ver = v
	}

	if ver > goa.Major {
		fail("cannot run goa %s on design using goa v%s\n", goa.Version(), *version)
	}

	if err := eval.Context.Errors; err != nil {
		fail(err.Error())
	}

	if err := eval.RunDSL(); err != nil {
		fail(err.Error())
	}

{{- range .CleanupDirs }}
	if err := os.RemoveAll({{ printf "%q" . }}); err != nil {
		fail(err.Error())
	}
{{- end }}
{{- if gt .DesignVersion 2 }}
	codegen.DesignVersion = ver
{{- end }}

	outputs, err := generator.Generate(*out, {{ printf "%q" .Command }}, *debug)
	if err != nil {
		fail(err.Error())
	}
	fmt.Println(strings.Join(outputs, "\n"))
}

func fail(msg string, vals ...any) {
	fmt.Fprintf(os.Stderr, msg, vals...)
	os.Exit(1)
}
`

