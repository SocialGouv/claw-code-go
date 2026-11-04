package bedrock

import (
	"encoding/json"
	"testing"

	"github.com/SocialGouv/claw-code-go/internal/api"
	"github.com/SocialGouv/claw-code-go/internal/testutil/schematest"
)

func TestReceivedSchemaGuarantee(t *testing.T) {
	schematest.Run(t, func(raw []byte) ([]byte, error) {
		var s api.InputSchema
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		req := api.CreateMessageRequest{Tools: []api.Tool{{Name: "guarantee", InputSchema: s}}}
		return schematest.ToolSchema(MarshalRequest(req))
	})
}
