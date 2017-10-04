package main

import (
	"flag"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	_ "github.com/bjhaid/oga/initializer"
	_ "github.com/bjhaid/oga/requester"

	_ "github.com/golang/glog"
)

type stringValue string

func (s *stringValue) Set(val string) error {
	*s = stringValue(val)
	return nil
}

func (s *stringValue) Get() interface{} { return string(*s) }

func (s *stringValue) String() string { return string(*s) }

func TestMain(t *testing.T) {
	flag.Parse()
	// All of this gymnastics is to eliminate test flags
	var s string
	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		if strings.HasPrefix(f.Name, "test.") {
			return
		}

		s += fmt.Sprintf("  -%s", f.Name) // Two spaces before -; see next two comments.
		name, usage := flag.UnquoteUsage(f)
		if len(name) > 0 {
			s += " " + name
		}
		// Boolean flags of one ASCII letter are so common we
		// treat them specially, putting their usage on the same line.
		if len(s) <= 4 { // space, space, '-', 'x'.
			s += "\t"
		} else {
			// Four spaces before the tab triggers good alignment
			// for both 4- and 8-space tab stops.
			s += "\n    \t"
		}
		s += usage
		if !isZeroValue(f, f.DefValue) {
			if _, ok := f.Value.(*stringValue); ok {
				// put quotes on the value
				s += fmt.Sprintf(" (default %q)", f.DefValue)
			} else {
				s += fmt.Sprintf(" (default %v)", f.DefValue)
			}
			s += "\n"
		}
	})

	expected := `
	  -alsologtostderr
	        log to standard error as well as files
	  -annotation string
	        The annotation to trigger initialization (default initializer.kubernetes.io/oga)
		-bot-name string
					The username of the oga bot as created in slack
	  -initializer-name string
	        The initializer name (default oga.initializer.kubernetes.io)
	  -kubeconfig string
	        (optional) absolute path to the kubeconfig file (default /Users/bjhaid/.kube/config)
	  -log_backtrace_at value
	        when logging hits line file:N, emit a stack trace
	  -log_dir string
	        If non-empty, write log files in this directory
	  -logtostderr
	        log to standard error instead of files
		-slack-token string
					Slack API token
	  -stderrthreshold value
	        logs at or above this threshold go to stderr
	  -v value
	        log level for V logs
	  -vmodule value
	        comma-separated list of pattern=N settings for file-filtered logging`

	re := regexp.MustCompile(`\s`)
	if re.ReplaceAllString(s, "") != re.ReplaceAllString(expected, "") {
		t.Errorf("Current flags: %s\n differ from the expected flags: %s\n", s,
			expected)
	}
}

type Value interface {
	String() string
	Set(string) error
}

func isZeroValue(flag *flag.Flag, value string) bool {
	// Build a zero value of the flag's Value type, and see if the
	// result of calling its String method equals the value passed in.
	// This works unless the Value type is itself an interface type.
	typ := reflect.TypeOf(flag.Value)
	var z reflect.Value
	if typ.Kind() == reflect.Ptr {
		z = reflect.New(typ.Elem())
	} else {
		z = reflect.Zero(typ)
	}
	if value == z.Interface().(Value).String() {
		return true
	}

	switch value {
	case "false":
		return true
	case "":
		return true
	case "0":
		return true
	}
	return false
}
