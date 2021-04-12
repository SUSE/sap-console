package agent

import (
	"fmt"
	"io/ioutil"

	"github.com/aquasecurity/bench-common/check"
	"github.com/pkg/errors"
)

type Checker func() (CheckResult, error)

func NewChecker(definitionsPath string) (Checker, error) {
	data, err := ioutil.ReadFile(definitionsPath)
	if err != nil {
		return nil, errors.Wrap(err, "could not read definitions file")
	}

	return func() (CheckResult, error) {
		result := CheckResult{}
		controls, err := check.NewControls(data, nil)
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

// I guess, the controls was private for some reason
func (cr *CheckResult) GetControls() *check.Controls {
	return cr.controls
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
