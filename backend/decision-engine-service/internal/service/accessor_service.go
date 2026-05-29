package service

import (
	"context"
	"fmt"
	"slices"

	domainast "github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/domain/ast"
	"github.com/Kwasi-itc/New-fraud-system/backend/decision-engine-service/internal/ports"
)

type AccessorSet struct {
	PayloadAccessors  []domainast.Node `json:"payload_accessors"`
	DatabaseAccessors []domainast.Node `json:"database_accessors"`
}

type AccessorService struct {
	scenarioRepo    ports.ScenarioRepository
	dataModelReader ports.DataModelReader
}

func NewAccessorService(
	scenarioRepo ports.ScenarioRepository,
	dataModelReader ports.DataModelReader,
) AccessorService {
	return AccessorService{
		scenarioRepo:    scenarioRepo,
		dataModelReader: dataModelReader,
	}
}

func (s AccessorService) ListByScenario(ctx context.Context, tenantID, scenarioID string) (AccessorSet, error) {
	scn, err := s.scenarioRepo.GetByID(ctx, tenantID, scenarioID)
	if err != nil {
		return AccessorSet{}, err
	}

	model, err := s.dataModelReader.GetTenantModel(ctx, tenantID)
	if err != nil {
		return AccessorSet{}, err
	}

	payloadAccessors, err := buildPayloadAccessors(scn.TriggerObjectType, model)
	if err != nil {
		return AccessorSet{}, err
	}
	databaseAccessors, err := buildDatabaseAccessors(scn.TriggerObjectType, model)
	if err != nil {
		return AccessorSet{}, err
	}

	return AccessorSet{
		PayloadAccessors:  payloadAccessors,
		DatabaseAccessors: databaseAccessors,
	}, nil
}

func buildPayloadAccessors(triggerObjectType string, model ports.TenantModel) ([]domainast.Node, error) {
	table, ok := model.Tables[triggerObjectType]
	if !ok {
		return nil, fmt.Errorf("trigger object type %q not found in tenant model", triggerObjectType)
	}

	out := make([]domainast.Node, 0, len(table.Fields))
	for fieldName := range table.Fields {
		out = append(out, domainast.Node{
			Function: "Payload",
			Children: []domainast.Node{{Constant: fieldName}},
		})
	}
	return out, nil
}

func buildDatabaseAccessors(triggerObjectType string, model ports.TenantModel) ([]domainast.Node, error) {
	triggerTable, ok := model.Tables[triggerObjectType]
	if !ok {
		return nil, fmt.Errorf("trigger object type %q not found in tenant model", triggerObjectType)
	}

	out := make([]domainast.Node, 0)
	var walk func(baseTable string, path []string, links map[string]ports.TenantModelLink, visited []string) error
	walk = func(baseTable string, path []string, links map[string]ports.TenantModelLink, visited []string) error {
		visited = append(visited, baseTable)
		for linkName, link := range links {
			table, ok := model.Tables[link.ParentTableName]
			if !ok {
				return fmt.Errorf("table %q not found in tenant model", link.ParentTableName)
			}
			if slices.Contains(visited, table.Name) {
				continue
			}

			pathForLink := append(append([]string{}, path...), linkName)
			for fieldName := range table.Fields {
				out = append(out, domainast.Node{
					Function: "DatabaseAccess",
					NamedChildren: map[string]domainast.Node{
						"tableName": {Constant: triggerObjectType},
						"fieldName": {Constant: fieldName},
						"path":      {Constant: pathForLink},
					},
				})
			}

			nextVisited := append(append([]string{}, visited...), table.Name)
			if err := walk(table.Name, pathForLink, table.LinksToSingle, nextVisited); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(triggerTable.Name, nil, triggerTable.LinksToSingle, nil); err != nil {
		return nil, err
	}
	return out, nil
}
