package agent

import (
	"fmt"
	"io"
	"io/ioutil"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/rtorrero/bench-common/check"
	log "github.com/sirupsen/logrus"
)

type Checker func() (CheckResult, error)

/* Remove
  '---
   controls:
	version: ...
	id: ...
	description: ...
	type: ...
	groups:
'
that comes before '    - id:...'
You need to do it to append the yaml to another yaml file
*/
func trim_yaml_header(yaml_text string) string {
	pattern := "\ngroups:"
	index := strings.Index(yaml_text, pattern)
	if index == -1 {
		log.Fatal("the yaml file doesn't contain the 'id:' field and is possibly broken")
	}
	index += len(pattern)
	return yaml_text[index:]
}

func NewChecker(definitionsPaths []string) (Checker, error) {
	var data [][]byte
	for _, path := range definitionsPaths {
		datum, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, errors.Wrap(err, "could not read definitions file")
		}
		data = append(data, datum)
	}
	return func() (CheckResult, error) {
		var result CheckResult
		first_yaml_file := string(data[0])
		// Appent all next files to the first
		for _, datum := range data[1:] {
			first_yaml_file += trim_yaml_header(string(datum))
		}

		controls, err := check.NewControls([]byte(first_yaml_file), nil)
		if err != nil {
			return result, errors.Wrap(err, "could not parse definitions file")
		}

		controls.RunGroup()

		result.controls = controls

		return result, nil
	}, nil
}

type CheckResult struct {
	controls *check.Controls
}

func (cr *CheckResult) CheckPrettyPrint(sw io.StringWriter) {
	// 1) Print descriptions
	controls := cr.controls
	remedations := ""
	sw.WriteString("[INFO] " + controls.ID + " " + controls.Description + "\n")
	for _, group := range controls.Groups {
		sw.WriteString("[INFO] " + group.ID + " " + group.Description + "\n")
		for _, check := range group.Checks {
			sw.WriteString("[" + string(check.State) + "] " + check.ID + " " + check.Description + "\n")
			if string(check.State) == "FAIL" {
				remedations += check.ID + " " + check.Remediation + "\n"
			}
		}
	}
	// 2) Print remedations
	sw.WriteString("\n== Remedations ==\n" + remedations)

	// 3) Print the summary
	sw.WriteString("\n== Summary ==\n")
	sw.WriteString(strconv.Itoa(controls.Summary.Pass) + " checks PASS\n")
	sw.WriteString(strconv.Itoa(controls.Summary.Fail) + " checks FAIL\n")
	sw.WriteString(strconv.Itoa(controls.Summary.Warn) + " checks WARN\n")
	sw.WriteString(strconv.Itoa(controls.Summary.Info) + " checks INFO\n")
}

func (r CheckResult) Summary() check.Summary {
	return r.controls.Summary
}

func (r CheckResult) MarshalJSON() ([]byte, error) {
	out, err := r.controls.JSON()
	if err != nil {
		return out, errors.Wrap(err, "could not convert check results to JSON")
	}
	return out, nil
}

func (r CheckResult) String() string {
	summary := r.controls.Summary
	return fmt.Sprintf("== Summary ==\n%d checks PASS\n%d checks FAIL\n%d checks WARN\n%d checks INFO\n",
		summary.Pass, summary.Fail, summary.Warn, summary.Info,
	)
}
