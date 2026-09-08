package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

const (
	distributionPolicyVersion = "distribution-v1"
	minimumAnalysisRows       = 1_000
	maximumAnalysisRows       = 250_000
	maximumFewValueDistinct   = 10_000
	maximumEffectiveEntities  = 1_000.0
	nearUniqueRatio           = 0.98
	nearUniqueExpectedMatches = 1.10
)

type DistributionAnalysisService struct{}

type DistributionValueReader interface {
	ReadValues(ctx context.Context, tenantID uuid.UUID, tableName, fieldName string, limit int) ([]*string, bool, error)
}

type DistributionAnalysis struct {
	PolicyVersion          string                         `json:"policy_version"`
	SuggestedCategory      datamodel.DistributionCategory `json:"suggested_category"`
	Reason                 string                         `json:"reason"`
	RowsAnalyzed           int64                          `json:"rows_analyzed"`
	NonNullRows            int64                          `json:"non_null_rows"`
	NullPercentage         float64                        `json:"null_percentage"`
	DistinctValues         int64                          `json:"distinct_values"`
	DistinctToNonNullRatio float64                        `json:"distinct_to_non_null_ratio"`
	LargestValueShare      float64                        `json:"largest_value_share"`
	Top2Share              float64                        `json:"top_2_share"`
	Top5Share              float64                        `json:"top_5_share"`
	Top10Share             float64                        `json:"top_10_share"`
	EffectiveEntityCount   float64                        `json:"effective_entity_count"`
	ExpectedSameValueRows  float64                        `json:"expected_same_value_rows"`
	FrequencyBands         map[string]int64               `json:"frequency_bands"`
	Truncated              bool                           `json:"truncated"`
}

func (DistributionAnalysisService) AnalyzeStored(ctx context.Context, reader DistributionValueReader, tenantID uuid.UUID, tableName, fieldName string) (DistributionAnalysis, error) {
	values, truncated, err := reader.ReadValues(ctx, tenantID, strings.TrimSpace(tableName), strings.TrimSpace(fieldName), maximumAnalysisRows)
	if err != nil {
		return DistributionAnalysis{}, err
	}
	counts := make(map[string]int64)
	var nonNull int64
	for _, value := range values {
		if value == nil || strings.TrimSpace(*value) == "" {
			continue
		}
		nonNull++
		counts[strings.TrimSpace(*value)]++
	}
	return analyzeDistribution(int64(len(values)), nonNull, counts, truncated), nil
}

func NewDistributionAnalysisService() DistributionAnalysisService {
	return DistributionAnalysisService{}
}

func (DistributionAnalysisService) AnalyzeCSV(reader io.Reader, column string) (DistributionAnalysis, error) {
	column = strings.TrimSpace(column)
	if column == "" {
		return DistributionAnalysis{}, fmt.Errorf("column is required")
	}

	csvReader := csv.NewReader(reader)
	csvReader.ReuseRecord = true
	header, err := csvReader.Read()
	if err != nil {
		return DistributionAnalysis{}, fmt.Errorf("read CSV header: %w", err)
	}
	columnIndex := -1
	for i, item := range header {
		if strings.TrimSpace(item) == column {
			columnIndex = i
			break
		}
	}
	if columnIndex < 0 {
		return DistributionAnalysis{}, fmt.Errorf("column %q was not found in the CSV header", column)
	}

	counts := make(map[string]int64)
	var rows, nonNull int64
	truncated := false
	for rows < maximumAnalysisRows {
		record, readErr := csvReader.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return DistributionAnalysis{}, fmt.Errorf("read CSV row %d: %w", rows+2, readErr)
		}
		rows++
		if columnIndex >= len(record) {
			continue
		}
		value := strings.TrimSpace(record[columnIndex])
		if value == "" {
			continue
		}
		nonNull++
		counts[value]++
	}
	if rows == maximumAnalysisRows {
		if _, readErr := csvReader.Read(); readErr == nil {
			truncated = true
		} else if readErr != io.EOF {
			return DistributionAnalysis{}, fmt.Errorf("check CSV analysis limit: %w", readErr)
		}
	}

	return analyzeDistribution(rows, nonNull, counts, truncated), nil
}

func analyzeDistribution(rows, nonNull int64, counts map[string]int64, truncated bool) DistributionAnalysis {
	analysis := DistributionAnalysis{
		PolicyVersion:     distributionPolicyVersion,
		SuggestedCategory: datamodel.DistributionUnknown,
		RowsAnalyzed:      rows,
		NonNullRows:       nonNull,
		DistinctValues:    int64(len(counts)),
		FrequencyBands:    map[string]int64{"1": 0, "2_to_5": 0, "6_to_10": 0, "11_to_100": 0, "over_100": 0},
		Truncated:         truncated,
	}
	if rows > 0 {
		analysis.NullPercentage = float64(rows-nonNull) / float64(rows)
	}
	if nonNull == 0 {
		analysis.Reason = "the sample has no non-null values"
		return analysis
	}

	frequencies := make([]int64, 0, len(counts))
	var squaredTotal float64
	for _, count := range counts {
		frequencies = append(frequencies, count)
		squaredTotal += float64(count) * float64(count)
		switch {
		case count == 1:
			analysis.FrequencyBands["1"]++
		case count <= 5:
			analysis.FrequencyBands["2_to_5"]++
		case count <= 10:
			analysis.FrequencyBands["6_to_10"]++
		case count <= 100:
			analysis.FrequencyBands["11_to_100"]++
		default:
			analysis.FrequencyBands["over_100"]++
		}
	}
	sort.Slice(frequencies, func(i, j int) bool { return frequencies[i] > frequencies[j] })
	share := func(limit int) float64 {
		if limit > len(frequencies) {
			limit = len(frequencies)
		}
		var total int64
		for _, count := range frequencies[:limit] {
			total += count
		}
		return float64(total) / float64(nonNull)
	}

	analysis.DistinctToNonNullRatio = float64(len(counts)) / float64(nonNull)
	analysis.LargestValueShare = share(1)
	analysis.Top2Share = share(2)
	analysis.Top5Share = share(5)
	analysis.Top10Share = share(10)
	analysis.ExpectedSameValueRows = squaredTotal / float64(nonNull)
	if squaredTotal > 0 {
		analysis.EffectiveEntityCount = math.Pow(float64(nonNull), 2) / squaredTotal
	}

	if nonNull < minimumAnalysisRows {
		analysis.Reason = fmt.Sprintf("at least %d non-null rows are required for an automatic suggestion", minimumAnalysisRows)
		return analysis
	}
	if analysis.DistinctToNonNullRatio >= nearUniqueRatio && analysis.ExpectedSameValueRows <= nearUniqueExpectedMatches {
		analysis.SuggestedCategory = datamodel.DistributionUniqueOrNearUnique
		analysis.Reason = "almost every sampled value identifies one row"
		return analysis
	}
	if len(counts) <= maximumFewValueDistinct && analysis.EffectiveEntityCount <= maximumEffectiveEntities {
		analysis.SuggestedCategory = datamodel.DistributionFewValueDominated
		analysis.Reason = "the sampled workload is concentrated in a bounded value set"
		return analysis
	}
	if analysis.Top10Share >= 0.80 && len(counts) > maximumFewValueDistinct {
		analysis.Reason = "a few values dominate, but the high-cardinality tail is not safe for Phase 1 precomputation"
		return analysis
	}
	analysis.SuggestedCategory = datamodel.DistributionHighlyDistributed
	analysis.Reason = "a typical value excludes most sampled rows"
	return analysis
}
