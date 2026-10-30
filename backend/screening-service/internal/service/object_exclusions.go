package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/domain/screening"
)

func objectExclusionScope(parent screening.Screening) (screening.ExclusionScope, bool) {
	if parent.IsManual || parent.ObjectType == "" || parent.ObjectID == "" || parent.Provider == "" {
		return screening.ExclusionScope{}, false
	}
	var request screening.SearchRequest
	if json.Unmarshal(parent.RequestJSON, &request) != nil || len(request.Queries) == 0 {
		return screening.ExclusionScope{}, false
	}
	raw, err := json.Marshal(request.Queries)
	if err != nil {
		return screening.ExclusionScope{}, false
	}
	hash := sha256.Sum256(raw)
	return screening.ExclusionScope{TenantID: parent.TenantID, ObjectType: parent.ObjectType, ObjectID: parent.ObjectID, Provider: parent.Provider, QueryFingerprint: hex.EncodeToString(hash[:])}, true
}
