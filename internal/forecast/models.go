package forecast

import (
	"math"
	"sort"
)

// StatisticalModel provides deterministic statistical forecasting
type StatisticalModel struct{}

// NewStatisticalModel creates a new statistical model
func NewStatisticalModel() *StatisticalModel {
	return &StatisticalModel{}
}

// CalculateMovingAverage calculates moving average over a window
func (m *StatisticalModel) CalculateMovingAverage(data []DataPoint, windowSize int) []float64 {
	if len(data) == 0 || windowSize <= 0 {
		return []float64{}
	}

	if windowSize > len(data) {
		windowSize = len(data)
	}

	result := make([]float64, len(data)-windowSize+1)

	for i := 0; i < len(data)-windowSize+1; i++ {
		sum := 0.0
		for j := 0; j < windowSize; j++ {
			sum += data[i+j].Value
		}
		result[i] = sum / float64(windowSize)
	}

	return result
}

// CalculateGrowthRate calculates the growth rate between two points
func (m *StatisticalModel) CalculateGrowthRate(previousValue, currentValue float64) float64 {
	if previousValue == 0 {
		if currentValue == 0 {
			return 0
		}
		return 1.0 // 100% growth from 0
	}

	return (currentValue - previousValue) / previousValue
}

// CalculateGrowthRateFromData calculates growth rate from data points
func (m *StatisticalModel) CalculateGrowthRateFromData(data []DataPoint) float64 {
	if len(data) < 2 {
		return 0
	}

	var totalGrowth float64
	for i := 1; i < len(data); i++ {
		rate := m.CalculateGrowthRate(data[i-1].Value, data[i].Value)
		totalGrowth += rate
	}

	return totalGrowth / float64(len(data)-1)
}

// PredictLinear performs linear regression prediction
func (m *StatisticalModel) PredictLinear(data []DataPoint, stepsAhead int) float64 {
	if len(data) < 2 {
		if len(data) == 1 {
			return data[0].Value
		}
		return 0
	}

	// Simple linear regression: y = a + bx
	n := float64(len(data))
	sumX := 0.0
	sumY := 0.0
	sumXY := 0.0
	sumX2 := 0.0

	for i, point := range data {
		x := float64(i)
		y := point.Value
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Calculate slope (b) and intercept (a)
	denominator := n*sumX2 - sumX*sumX
	if denominator == 0 {
		return data[len(data)-1].Value
	}

	slope := (n*sumXY - sumX*sumY) / denominator
	intercept := (sumY - slope*sumX) / n

	// Predict at steps ahead
	xPredict := float64(len(data) + stepsAhead - 1)
	return intercept + slope*xPredict
}

// CalculateRollingWindow calculates statistics over a rolling window
func (m *StatisticalModel) CalculateRollingWindow(data []DataPoint, windowSize int) map[string]float64 {
	if len(data) == 0 {
		return map[string]float64{
			"min":    0,
			"max":    0,
			"mean":   0,
			"stddev": 0,
		}
	}

	if windowSize > len(data) {
		windowSize = len(data)
	}

	// Use last windowSize points
	start := len(data) - windowSize
	if start < 0 {
		start = 0
	}

	values := make([]float64, 0, windowSize)
	for i := start; i < len(data); i++ {
		values = append(values, data[i].Value)
	}

	return m.calculateStats(values)
}

// DetectTrend detects the trend in data
func (m *StatisticalModel) DetectTrend(data []DataPoint) Trend {
	if len(data) < 3 {
		return TrendStable
	}

	growthRate := m.CalculateGrowthRateFromData(data)

	// Strong increasing trend
	if growthRate > 0.1 {
		return TrendIncreasing
	}

	// Strong decreasing trend
	if growthRate < -0.1 {
		return TrendDecreasing
	}

	// Stable trend
	return TrendStable
}

// DetectAnomaly detects outliers in data
func (m *StatisticalModel) DetectAnomaly(data []DataPoint) bool {
	if len(data) < 4 {
		return false
	}

	stats := m.CalculateRollingWindow(data, len(data))
	mean := stats["mean"]
	stddev := stats["stddev"]

	if stddev == 0 {
		return false
	}

	// Check if last point is more than 3 standard deviations away
	lastValue := data[len(data)-1].Value
	zScore := math.Abs((lastValue - mean) / stddev)

	return zScore > 3.0
}

// ForecastWithConfidence generates forecast with confidence interval
func (m *StatisticalModel) ForecastWithConfidence(data []DataPoint, stepsAhead int) (float64, float64) {
	if len(data) < 2 {
		return 0, 0
	}

	predicted := m.PredictLinear(data, stepsAhead)

	// Calculate confidence based on data consistency
	confidence := m.calculateConfidence(data)

	return predicted, confidence
}

// Helper functions

func (m *StatisticalModel) calculateStats(values []float64) map[string]float64 {
	if len(values) == 0 {
		return map[string]float64{
			"min":    0,
			"max":    0,
			"mean":   0,
			"stddev": 0,
		}
	}

	sort.Float64s(values)

	min := values[0]
	max := values[len(values)-1]

	// Calculate mean
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	// Calculate standard deviation
	variance := 0.0
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	stddev := math.Sqrt(variance)

	return map[string]float64{
		"min":    min,
		"max":    max,
		"mean":   mean,
		"stddev": stddev,
	}
}

func (m *StatisticalModel) calculateConfidence(data []DataPoint) float64 {
	if len(data) < 2 {
		return 0.5
	}

	// Base confidence on data points count and trend consistency
	baseConfidence := 0.5

	// More data points = higher confidence
	if len(data) >= 10 {
		baseConfidence += 0.3
	} else if len(data) >= 5 {
		baseConfidence += 0.15
	}

	// Check for anomalies - reduces confidence
	if m.DetectAnomaly(data) {
		baseConfidence -= 0.1
	}

	// Check coefficient of variation (consistency)
	stats := m.calculateStats(extractValues(data))
	if stats["mean"] > 0 && stats["stddev"] > 0 {
		cv := stats["stddev"] / stats["mean"]
		if cv > 1.0 {
			baseConfidence -= 0.2
		} else if cv > 0.5 {
			baseConfidence -= 0.1
		}
	}

	// Cap confidence
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}
	if baseConfidence < 0.1 {
		baseConfidence = 0.1
	}

	return baseConfidence
}

func extractValues(data []DataPoint) []float64 {
	values := make([]float64, len(data))
	for i, point := range data {
		values[i] = point.Value
	}
	return values
}
