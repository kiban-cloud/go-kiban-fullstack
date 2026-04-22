package pagination

import (
	"slices"
	"time"

	errorsWrapper "github.com/pkg/errors"
)

type FiltersSorters struct {
	Filters []Filters `json:"filters,omitempty"`
	Sorters []string  `json:"sorters,omitempty"`
}

type Filters struct {
	Name string     `json:"name"`
	Type FilterType `json:"type"`
	Enum []string   `json:"enum,omitempty"`
}

type FilterType string

const (
	FilterTypeString  FilterType = "string"
	FilterTypeNumber  FilterType = "number"
	FilterTypeDate    FilterType = "date"
	FilterTypeBoolean FilterType = "boolean"
	FilterTypeEnum    FilterType = "enum"
)

func ParseDateFilters(filters map[string]any, dateFields []string, loc *time.Location) (map[string]any, error) {
	result := make(map[string]any, len(filters))

	for field, value := range filters {
		if !slices.Contains(dateFields, field) {
			result[field] = value
			continue
		}
		v, ok := value.(map[string]any)
		if !ok {
			return nil, errorsWrapper.Wrap(ErrInvalidDateFormat, "error when trying to parse date filters")
		}
		newValue, err := parseDateFieldValue(v, loc)
		if err != nil {
			return nil, err
		}
		result[field] = newValue
	}
	return result, nil
}

func parseDateFieldValue(expression map[string]any, loc *time.Location) (map[string]any, error) {
	result := make(map[string]any, len(expression))
	for cond, val := range expression {
		s, ok := val.(string)
		if !ok {
			return nil, errorsWrapper.Wrap(ErrInvalidDateFormat, "error when trying to parse date filters")
		}
		t, err := time.ParseInLocation("2006-01-02", s, loc)
		if err != nil {
			return nil, errorsWrapper.Wrap(ErrInvalidDateFormat, "error when trying to parse date filters")
		}
		result[cond] = t
	}
	return result, nil
}
