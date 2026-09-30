package form

import "testing"

func TestSpecValidatesControlsAndValues(t *testing.T) {
	spec := Spec{Fields: []Field{
		{ID: "project_id", Label: "Project", Type: Select, Options: []Option{{ID: "123", Name: "Web"}}, Remember: true},
		{ID: "hours", Label: "Hours", Type: Hours},
	}}
	if err := spec.ValidateValues(map[string]string{"project_id": "123", "hours": "1.25"}); err != nil {
		t.Fatal(err)
	}
	for _, values := range []map[string]string{{"project_id": "gone", "hours": "1"}, {"project_id": "123", "hours": "0"}, {"project_id": "123", "hours": "NaN"}} {
		if err := spec.ValidateValues(values); err == nil {
			t.Fatalf("accepted %v", values)
		}
	}
	bad := []Spec{
		{},
		{Fields: []Field{{ID: "bad-id", Label: "Bad", Type: Hours}}},
		{Fields: []Field{{ID: "x", Label: "X", Type: Select}}},
		{Fields: []Field{{ID: "x", Label: "X", Type: Hours, Remember: true}}},
		{Fields: []Field{{ID: "x", Label: "X", Type: Hours}, {ID: "x", Label: "Again", Type: Hours}}},
	}
	for _, candidate := range bad {
		if err := candidate.Validate(); err == nil {
			t.Fatalf("accepted bad spec %+v", candidate)
		}
	}
}
