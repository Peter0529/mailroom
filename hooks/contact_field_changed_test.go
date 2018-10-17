package hooks

import (
	"testing"

	"github.com/nyaruka/goflow/assets"
	"github.com/nyaruka/goflow/flows"
	"github.com/nyaruka/goflow/flows/actions"
	"github.com/nyaruka/mailroom/models"
)

func TestContactFieldChanged(t *testing.T) {
	genderUUID := models.FieldUUID("0ecedc66-12d8-4a46-bcc7-8168f77e4ff6")
	gender := assets.NewFieldReference("gender", "Gender")

	ageUUID := models.FieldUUID("4f1b24f7-6320-4a86-bcb6-036e7a736094")
	age := assets.NewFieldReference("age", "Age")

	tcs := []HookTestCase{
		HookTestCase{
			Actions: ContactActionMap{
				Cathy: []flows.Action{
					actions.NewSetContactFieldAction(newActionUUID(), gender, "Male"),
					actions.NewSetContactFieldAction(newActionUUID(), gender, "Female"),
					actions.NewSetContactFieldAction(newActionUUID(), age, ""),
				},
				Evan: []flows.Action{
					actions.NewSetContactFieldAction(newActionUUID(), gender, "Male"),
					actions.NewSetContactFieldAction(newActionUUID(), gender, ""),
					actions.NewSetContactFieldAction(newActionUUID(), age, "30"),
				},
				Bob: []flows.Action{
					actions.NewSetContactFieldAction(newActionUUID(), gender, ""),
					actions.NewSetContactFieldAction(newActionUUID(), gender, "Male"),
					actions.NewSetContactFieldAction(newActionUUID(), age, "Old"),
				},
			},
			Assertions: []SQLAssertion{
				SQLAssertion{
					SQL:   `select count(*) from contacts_contact where id = $1 AND fields->$2 = '{"text":"Female"}'::jsonb`,
					Args:  []interface{}{Cathy, genderUUID},
					Count: 1,
				},
				SQLAssertion{
					SQL:   `select count(*) from contacts_contact where id = $1 AND NOT fields?$2`,
					Args:  []interface{}{Cathy, ageUUID},
					Count: 1,
				},
				SQLAssertion{
					SQL:   `select count(*) from contacts_contact where id = $1 AND NOT fields?$2`,
					Args:  []interface{}{Evan, genderUUID},
					Count: 1,
				},
				SQLAssertion{
					SQL:   `select count(*) from contacts_contact where id = $1 AND fields->$2 = '{"text":"30", "number": 30}'::jsonb`,
					Args:  []interface{}{Evan, ageUUID},
					Count: 1,
				},
				SQLAssertion{
					SQL:   `select count(*) from contacts_contact where id = $1 AND fields->$2 = '{"text":"Male"}'::jsonb`,
					Args:  []interface{}{Bob, genderUUID},
					Count: 1,
				},
				SQLAssertion{
					SQL:   `select count(*) from contacts_contact where id = $1 AND fields->$2 = '{"text":"Old"}'::jsonb`,
					Args:  []interface{}{Bob, ageUUID},
					Count: 1,
				},
				SQLAssertion{
					SQL:   `select count(*) from contacts_contact where id = $1 AND NOT fields?$2`,
					Args:  []interface{}{Bob, "unknown"},
					Count: 1,
				},
			},
		},
	}

	RunActionTestCases(t, tcs)
}