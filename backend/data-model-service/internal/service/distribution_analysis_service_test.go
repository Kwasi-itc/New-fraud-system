package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Kwasi-itc/New-fraud-system/backend/data-model-service/internal/domain/datamodel"
)

func TestDistributionAnalysisClassifiesConcentratedSample(t *testing.T) {
	var input strings.Builder
	input.WriteString("merchant_id,amount\n")
	for i := 0; i < 1_200; i++ {
		fmt.Fprintf(&input, "merchant-%d,%d\n", i%3, i)
	}

	analysis, err := NewDistributionAnalysisService().AnalyzeCSV(strings.NewReader(input.String()), "merchant_id")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.SuggestedCategory != datamodel.DistributionFewValueDominated {
		t.Fatalf("category = %q", analysis.SuggestedCategory)
	}
	if analysis.DistinctValues != 3 {
		t.Fatalf("distinct values = %d", analysis.DistinctValues)
	}
}

func TestDistributionAnalysisClassifiesUniqueSample(t *testing.T) {
	var input strings.Builder
	input.WriteString("transaction_id\n")
	for i := 0; i < 1_200; i++ {
		fmt.Fprintf(&input, "txn-%d\n", i)
	}

	analysis, err := NewDistributionAnalysisService().AnalyzeCSV(strings.NewReader(input.String()), "transaction_id")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.SuggestedCategory != datamodel.DistributionUniqueOrNearUnique {
		t.Fatalf("category = %q", analysis.SuggestedCategory)
	}
}

func TestDistributionAnalysisLeavesSmallSampleUnknown(t *testing.T) {
	analysis, err := NewDistributionAnalysisService().AnalyzeCSV(strings.NewReader("source_id\nMTN\nMTN\n"), "source_id")
	if err != nil {
		t.Fatal(err)
	}
	if analysis.SuggestedCategory != datamodel.DistributionUnknown {
		t.Fatalf("category = %q", analysis.SuggestedCategory)
	}
}

func TestDistributionAnalysisClassifiesDistributedRepeatedValues(t *testing.T) {
	counts := make(map[string]int64, 2_000)
	for i := 0; i < 2_000; i++ {
		counts[fmt.Sprintf("account-%d", i)] = 2
	}
	analysis := analyzeDistribution(4_000, 4_000, counts, false)
	if analysis.SuggestedCategory != datamodel.DistributionHighlyDistributed {
		t.Fatalf("category = %q, want highly_distributed", analysis.SuggestedCategory)
	}
}

func TestDistributionAnalysisKeepsDominatedHighCardinalityTailUnknown(t *testing.T) {
	counts := make(map[string]int64, 12_010)
	for i := 0; i < 10; i++ {
		counts[fmt.Sprintf("dominant-%d", i)] = 4_800
	}
	for i := 0; i < 12_000; i++ {
		counts[fmt.Sprintf("tail-%d", i)] = 1
	}
	analysis := analyzeDistribution(60_000, 60_000, counts, false)
	if analysis.SuggestedCategory != datamodel.DistributionUnknown {
		t.Fatalf("category = %q, want unknown for an unsafe mixed tail", analysis.SuggestedCategory)
	}
}

type storedDistributionReaderStub struct {
	values    []*string
	truncated bool
}

func (s storedDistributionReaderStub) ReadValues(context.Context, uuid.UUID, string, string, int) ([]*string, bool, error) {
	return s.values, s.truncated, nil
}

func TestDistributionAnalysisUsesBoundedStoredValues(t *testing.T) {
	merchantA := "merchant-a"
	merchantB := "merchant-b"
	values := make([]*string, 1_200)
	for i := range values {
		if i%4 == 0 {
			values[i] = &merchantB
		} else {
			values[i] = &merchantA
		}
	}
	analysis, err := NewDistributionAnalysisService().AnalyzeStored(
		context.Background(), storedDistributionReaderStub{values: values, truncated: true}, uuid.New(), "transactions", "merchant_id",
	)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.SuggestedCategory != datamodel.DistributionFewValueDominated || !analysis.Truncated {
		t.Fatalf("analysis = %#v", analysis)
	}
}
