package scoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
)

type Config struct {
	PolicyVersion                     string  `json:"policyVersion"`
	GreenMaxTrafficRatio              float64 `json:"greenMaxTrafficRatio"`
	YellowMaxTrafficRatio             float64 `json:"yellowMaxTrafficRatio"`
	OrangeMaxTrafficRatio             float64 `json:"orangeMaxTrafficRatio"`
	MinimumGeometrySimilarity         float64 `json:"minimumGeometrySimilarity"`
	MinimumRouteConfidence            float64 `json:"minimumRouteConfidence"`
	UnknownHighConfidenceMaxPercent   float64 `json:"unknownHighConfidenceMaxPercent"`
	UnknownMediumConfidenceMaxPercent float64 `json:"unknownMediumConfidenceMaxPercent"`
	BalancedTrafficWeight             float64 `json:"balancedTrafficWeight"`
	BalancedETAWeight                 float64 `json:"balancedEtaWeight"`
	BalancedDistanceWeight            float64 `json:"balancedDistanceWeight"`
	BalancedUncertaintyWeight         float64 `json:"balancedUncertaintyWeight"`
	BalancedTollWeight                float64 `json:"balancedTollWeight"`
	GreenestExtraTimePenalty          float64 `json:"greenestExtraTimePenalty"`
}

func DefaultConfig() Config {
	return Config{
		PolicyVersion:                     "greenroute-scoring-v2.0.0",
		GreenMaxTrafficRatio:              1.15,
		YellowMaxTrafficRatio:             1.35,
		OrangeMaxTrafficRatio:             1.65,
		MinimumGeometrySimilarity:         0.72,
		MinimumRouteConfidence:            0.35,
		UnknownHighConfidenceMaxPercent:   5,
		UnknownMediumConfidenceMaxPercent: 25,
		BalancedTrafficWeight:             0.40,
		BalancedETAWeight:                 0.25,
		BalancedDistanceWeight:            0.15,
		BalancedUncertaintyWeight:         0.15,
		BalancedTollWeight:                0.05,
		GreenestExtraTimePenalty:          0.15,
	}
}

func (c Config) Validate() error {
	numbers := []float64{
		c.GreenMaxTrafficRatio, c.YellowMaxTrafficRatio, c.OrangeMaxTrafficRatio,
		c.MinimumGeometrySimilarity, c.MinimumRouteConfidence,
		c.UnknownHighConfidenceMaxPercent, c.UnknownMediumConfidenceMaxPercent,
		c.BalancedTrafficWeight, c.BalancedETAWeight, c.BalancedDistanceWeight,
		c.BalancedUncertaintyWeight, c.BalancedTollWeight, c.GreenestExtraTimePenalty,
	}
	for _, number := range numbers {
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return ErrInvalidScoringConfig
		}
	}
	if c.PolicyVersion == "" || c.GreenMaxTrafficRatio <= 1 || c.YellowMaxTrafficRatio <= c.GreenMaxTrafficRatio || c.OrangeMaxTrafficRatio <= c.YellowMaxTrafficRatio {
		return ErrInvalidScoringConfig
	}
	if c.MinimumGeometrySimilarity < 0 || c.MinimumGeometrySimilarity > 1 || c.MinimumRouteConfidence < 0 || c.MinimumRouteConfidence > 1 {
		return ErrInvalidScoringConfig
	}
	if c.UnknownHighConfidenceMaxPercent < 0 || c.UnknownMediumConfidenceMaxPercent < c.UnknownHighConfidenceMaxPercent || c.UnknownMediumConfidenceMaxPercent > 100 {
		return ErrInvalidScoringConfig
	}
	weights := []float64{c.BalancedTrafficWeight, c.BalancedETAWeight, c.BalancedDistanceWeight, c.BalancedUncertaintyWeight, c.BalancedTollWeight}
	weightSum := 0.0
	for _, weight := range weights {
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
			return ErrInvalidScoringConfig
		}
		weightSum += weight
	}
	if math.Abs(weightSum-1) > 0.000001 || c.GreenestExtraTimePenalty < 0 || math.IsNaN(c.GreenestExtraTimePenalty) || math.IsInf(c.GreenestExtraTimePenalty, 0) {
		return ErrInvalidScoringConfig
	}
	return nil
}

func LoadConfigFile(path string) (Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read scoring policy: %w", err)
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode scoring policy: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return Config{}, fmt.Errorf("decode scoring policy: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate scoring policy: %w", err)
	}
	return config, nil
}
