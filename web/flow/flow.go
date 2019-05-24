package flow

import (
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/nyaruka/goflow/flows"
	"github.com/nyaruka/goflow/flows/definition"
	"github.com/nyaruka/goflow/legacy"
	"github.com/nyaruka/goflow/utils"
	"github.com/nyaruka/mailroom/config"
	"github.com/nyaruka/mailroom/models"
	"github.com/nyaruka/mailroom/web"

	"github.com/jmoiron/sqlx"
	"github.com/pkg/errors"
)

func init() {
	web.RegisterJSONRoute(http.MethodPost, "/mr/flow/migrate", web.RequireAuthToken(handleMigrate))
	web.RegisterJSONRoute(http.MethodPost, "/mr/flow/validate", web.RequireAuthToken(handleValidate))
	web.RegisterJSONRoute(http.MethodPost, "/mr/flow/inspect", web.RequireAuthToken(handleInspect))
	web.RegisterJSONRoute(http.MethodPost, "/mr/flow/clone", web.RequireAuthToken(handleClone))
}

// Migrates a legacy flow to the new flow definition specification
//
//   {
//     "flow": {"uuid": "468621a8-32e6-4cd2-afc1-04416f7151f0", "action_sets": [], ...},
//     "include_ui": false
//   }
//
type migrateRequest struct {
	Flow      json.RawMessage `json:"flow"            validate:"required"`
	IncludeUI *bool           `json:"include_ui"`
}

func handleMigrate(ctx context.Context, s *web.Server, r *http.Request) (interface{}, int, error) {
	request := &migrateRequest{}
	if err := readRequest(r, request); err != nil {
		return nil, http.StatusBadRequest, err
	}

	legacyFlow, err := legacy.ReadLegacyFlow(request.Flow)
	if err != nil {
		return nil, http.StatusBadRequest, errors.Wrapf(err, "error reading legacy flow")
	}

	includeUI := request.IncludeUI == nil || *request.IncludeUI

	flow, err := legacyFlow.Migrate(includeUI, "https://"+config.Mailroom.AttachmentDomain)
	if err != nil {
		return nil, http.StatusBadRequest, errors.Wrapf(err, "error migrating legacy flow")
	}

	return flow, http.StatusOK, nil
}

// Validates a flow. If validation fails, we return the error. If it succeeds, we return
// the valid definition which will now include extracted dependencies. The provided flow
// definition can be in either legacy or new format, but the returned definition will
// always be in the new format. `org_id` is optional and determines whether we load and
// pass assets to the flow validation to find missing assets.
//
// Note that a invalid request to this endpoint will return a 400 status code, but that a
// valid request with a flow that fails validation will return a 422 status code.
//
//   {
//     "org_id": 1,
//     "flow": { "uuid": "468621a8-32e6-4cd2-afc1-04416f7151f0", "nodes": [...]}
//   }
//
type validateRequest struct {
	OrgID models.OrgID    `json:"org_id"`
	Flow  json.RawMessage `json:"flow"   validate:"required"`
}

func handleValidate(ctx context.Context, s *web.Server, r *http.Request) (interface{}, int, error) {
	request := &validateRequest{}
	if err := readRequest(r, request); err != nil {
		return nil, http.StatusBadRequest, err
	}

	var flowDef = request.Flow
	var err error

	// migrate definition if it is in legacy format
	if legacy.IsLegacyDefinition(flowDef) {
		flowDef, err = legacy.MigrateLegacyDefinition(flowDef, "https://"+config.Mailroom.AttachmentDomain)
		if err != nil {
			return nil, http.StatusUnprocessableEntity, err
		}
	}

	// try to read the flow definition which will fail if it's invalid
	flow, err := definition.ReadFlow(flowDef)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	// if we have an org ID, do asset validation
	if request.OrgID != models.NilOrgID {
		status, err := validate(s.CTX, s.DB, request.OrgID, flow)
		if err != nil {
			return nil, status, err
		}
	}

	// this endpoint returns inspection results inside the definition
	result, err := flow.MarshalWithInfo()
	if err != nil {
		return nil, http.StatusInternalServerError, errors.Wrapf(err, "unable to marshal flow")
	}

	return json.RawMessage(result), http.StatusOK, nil
}

// Inspects a flow.
//
//   {
//     "flow": { "uuid": "468621a8-32e6-4cd2-afc1-04416f7151f0", "nodes": [...]}
//   }
//
type inspectRequest struct {
	Flow json.RawMessage `json:"flow" validate:"required"`
}

func handleInspect(ctx context.Context, s *web.Server, r *http.Request) (interface{}, int, error) {
	request := &inspectRequest{}
	if err := readRequest(r, request); err != nil {
		return nil, http.StatusBadRequest, err
	}

	// try to read the flow definition which will fail if it's invalid
	flow, err := definition.ReadFlow(request.Flow)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	info := flow.Inspect()

	return info, http.StatusOK, nil
}

// Clones a flow, replacing all UUIDs with either the given mapping or new random UUIDs.
// If `validate_with_org_id` is specified then the cloned flow will be validated against
// the assets of that org.
//
//   {
//     "dependency_mapping": {
//       "4ee4189e-0c06-4b00-b54f-5621329de947": "db31d23f-65b8-4518-b0f6-45638bfbbbf2",
//       "723e62d8-a544-448f-8590-1dfd0fccfcd4": "f1fd861c-9e75-4376-a829-dcf76db6e721"
//     },
//     "flow": { "uuid": "468621a8-32e6-4cd2-afc1-04416f7151f0", "nodes": [...]},
//     "validate_with_org_id": 1
//   }
//
type cloneRequest struct {
	DependencyMapping map[utils.UUID]utils.UUID `json:"dependency_mapping"`
	Flow              json.RawMessage           `json:"flow" validate:"required"`
	ValidateWithOrgID models.OrgID              `json:"validate_with_org_id"`
}

func handleClone(ctx context.Context, s *web.Server, r *http.Request) (interface{}, int, error) {
	request := &cloneRequest{}
	if err := readRequest(r, request); err != nil {
		return nil, http.StatusBadRequest, err
	}

	// try to read the flow definition which will fail if it's invalid
	flow, err := definition.ReadFlow(request.Flow)
	if err != nil {
		return nil, http.StatusUnprocessableEntity, err
	}

	clone := flow.Clone(request.DependencyMapping)

	// if we have an org ID, do asset validation on the new clone
	if request.ValidateWithOrgID != models.NilOrgID {
		status, err := validate(s.CTX, s.DB, request.ValidateWithOrgID, clone)
		if err != nil {
			return nil, status, err
		}
	}

	return clone, http.StatusOK, nil
}

func readRequest(r *http.Request, request interface{}) error {
	body, err := ioutil.ReadAll(io.LimitReader(r.Body, web.MaxRequestBytes))
	if err != nil {
		return err
	}

	if err := r.Body.Close(); err != nil {
		return err
	}

	if err := utils.UnmarshalAndValidate(body, request); err != nil {
		return errors.Wrapf(err, "error unmarshalling request")
	}

	return nil
}

func validate(ctx context.Context, db *sqlx.DB, orgID models.OrgID, flow flows.Flow) (int, error) {
	org, err := models.NewOrgAssets(ctx, db, orgID, nil)
	if err != nil {
		return http.StatusBadRequest, err
	}

	sa, err := models.NewSessionAssets(org)
	if err != nil {
		return http.StatusInternalServerError, errors.Wrapf(err, "unable build session assets")
	}

	if err := flow.Validate(sa, nil); err != nil {
		return http.StatusUnprocessableEntity, err
	}

	return 0, nil
}
