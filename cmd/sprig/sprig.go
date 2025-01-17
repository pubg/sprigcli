package sprig

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"text/template"

	"helm.sh/helm/v3/pkg/strvals"
	"sigs.k8s.io/yaml"

	"github.com/Masterminds/sprig/v3"
	"github.com/spf13/cobra"
)

var Version = "unknown"

type sprigCommand struct {
	valueFiles    valueFiles
	stdInTemplate bool
	envValues     bool
	dryRun        bool
	version       bool
	values        []string
	target        string
}

func NewSprigCmd() *cobra.Command {
	sprigCmd := &sprigCommand{}

	sprigCLI := &cobra.Command{
		Use:   "sprig",
		Short: "A CLI for golang text/template processing",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sprigCmd.stdInTemplate {
				if len(args) > 0 {
					return fmt.Errorf("you must provide template from stdin when using stdin mode")
				}
			} else {
				if len(args) > 1 {
					return fmt.Errorf("too many parameters, you must provide only one template file")
				}
				if len(args) == 0 {
					return fmt.Errorf("there are no parameters, you must provide one template file")
				}
				sprigCmd.target = args[0]
			}
			return sprigCmd.run()
		},
	}
	sprigCLI.Flags().BoolVar(&sprigCmd.stdInTemplate, "stdin", false, "read template use stdin")
	sprigCLI.Flags().BoolVar(&sprigCmd.envValues, "env", false, "pull template values from the environment")
	sprigCLI.Flags().BoolVar(&sprigCmd.version, "version", false, "print version and exit")
	sprigCLI.Flags().VarP(&sprigCmd.valueFiles, "values", "f", "specify values in YAML file (can specify multiple, comma separated)")
	sprigCLI.Flags().StringArrayVar(&sprigCmd.values, "set", []string{}, "set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")

	return sprigCLI
}

type valueFiles []string

func (v *valueFiles) String() string {
	return fmt.Sprint(*v)
}

func (v *valueFiles) Type() string {
	return "valueFiles"
}

func (v *valueFiles) Set(value string) error {
	for _, filePath := range strings.Split(value, ",") {
		*v = append(*v, filePath)
	}
	return nil
}

func (i *sprigCommand) vals() (map[string]interface{}, error) {
	base := map[string]interface{}{}

	// User specified a values files via -f/--values
	for _, filePath := range i.valueFiles {
		currentMap := map[string]interface{}{}
		bytes, err := ioutil.ReadFile(filePath)
		if err != nil {
			return nil, err
		}

		if err := yaml.Unmarshal(bytes, &currentMap); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %s", filePath, err)
		}
		// Merge with the previous map
		base = mergeMaps(base, currentMap)
	}

	// User specified a value via --set
	for _, value := range i.values {
		if err := strvals.ParseInto(value, base); err != nil {
			return nil, fmt.Errorf("failed parsing --set data: %s", err)
		}
	}

	// Environment set stuff
	if i.envValues {
		envMap := map[string]interface{}{}
		envVars := os.Environ()
		for _, envVar := range envVars {
			splitVar := strings.SplitN(envVar, "=", 2)
			envMap[splitVar[0]] = splitVar[1]
		}
		base = mergeMaps(base, envMap)
	}

	return base, nil
}

func (i *sprigCommand) run() error {
	if i.version {
		fmt.Println("sprig: " + Version)
		return nil
	}

	vals, err := i.vals()
	if err != nil {
		return err
	}

	var r io.Reader
	if i.stdInTemplate {
		if shouldReadStdin() {
			r = os.Stdin
		} else {
			return fmt.Errorf("stdinTemplate option is enabled, but pipe is not open")
		}
	} else {
		if i.target == "" {
			return fmt.Errorf("must provide a file to template")
		}
		r, err = os.Open(i.target)
		if err != nil {
			return err
		}
	}

	templateData, err := ioutil.ReadAll(r)
	if err != nil {
		return fmt.Errorf("could not read input: %v", err)
	}

	tmpl, err := template.New("gotmpl").Funcs(sprig.TxtFuncMap()).Parse(string(templateData))
	if err != nil {
		return fmt.Errorf("could not parse template: %v", err)
	}
	tmpl.Option("missingkey=error")

	return tmpl.Execute(os.Stdout, vals)
}

// Merges source and destination map, preferring values from the source map
func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// shouldReadStdin determines if stdin should be considered a valid source of data for templating.
func shouldReadStdin() bool {
	stdinStat, err := os.Stdin.Stat()
	if err != nil {
		panic(err)
	}
	return stdinStat.Mode()&os.ModeCharDevice == 0
}
