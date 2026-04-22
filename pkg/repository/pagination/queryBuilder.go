package repository_pagination

import (
	"slices"

	domain_pagination "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/pagination"

	errorsWrapper "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type QueryBuilder struct {
	tenantID     string
	isBackoffice bool
}

func NewQueryBuilder(tenantID string, isBackoffice bool) *QueryBuilder {
	if isBackoffice && tenantID != "" {
		panic(errorsWrapper.New("New QueryBuilder: tenantID cannot be provided for backoffice queries"))
	}
	return &QueryBuilder{tenantID: tenantID, isBackoffice: isBackoffice}
}

func (qb *QueryBuilder) BuildFilter(params domain_pagination.Params, allowedFilters []string) (bson.M, error) {
	filter := bson.M{}
	if !qb.isBackoffice {
		filter["tenantId"] = qb.tenantID
	}

	for key, value := range params.Filters {
		if !slices.Contains(allowedFilters, key) {
			continue
		}

		switch v := value.(type) {
		case string:
			if v != "" {
				filter[key] = bson.M{"$regex": v, "$options": "i"}
			}
		case map[string]any:
			filter[key] = v
		default:
			filter[key] = value
		}
	}

	return filter, nil
}

func (qb *QueryBuilder) BuildSort(params domain_pagination.Params, allowedFields []string) bson.D {
	sort := bson.D{}

	for field, direction := range params.Sort {
		if !slices.Contains(allowedFields, field) {
			continue
		}

		if direction != 1 && direction != -1 {
			direction = 1
		}

		sort = append(sort, bson.E{Key: field, Value: direction})
	}

	if len(sort) == 0 {
		sort = bson.D{{Key: "createdAt", Value: -1}}
	}

	return sort
}

func (qb *QueryBuilder) BuildFindOptions(params domain_pagination.Params, allowedSortFields []string) *options.FindOptionsBuilder {
	opts := options.Find()
	opts.SetSkip(params.GetSkip())
	opts.SetLimit(params.GetLimit())
	opts.SetSort(qb.BuildSort(params, allowedSortFields))

	return opts
}
