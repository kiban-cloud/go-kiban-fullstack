package repository_pagination

import (
	"context"
	"math"

	domain_pagination "github.com/kiban-cloud/go-kiban-fullstack/pkg/domain/pagination"

	errorsWrapper "github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type GenericRepository[T any] struct {
	collection *mongo.Collection
}

func NewGenericRepository[T any](collection *mongo.Collection) *GenericRepository[T] {
	return &GenericRepository[T]{
		collection: collection,
	}
}

func (r *GenericRepository[T]) FindWithQueryByTenant(ctx context.Context, tenantID string, isBackoffice bool, params domain_pagination.Params, allowedFilters []string, allowedSortFields []string) (*domain_pagination.Result[T], error) {
	params.Validate()
	qb := NewQueryBuilder(tenantID, isBackoffice)

	filter, err := qb.BuildFilter(params, allowedFilters)
	if err != nil {
		return nil, errorsWrapper.Wrap(err, "error when trying to build filter")
	}
	opts := qb.BuildFindOptions(params, allowedSortFields)

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return &domain_pagination.Result[T]{
			Data:       []T{},
			Total:      0,
			Page:       params.Page,
			PageSize:   params.PageSize,
			TotalPages: 0,
		}, nil
	}

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, errorsWrapper.Wrap(err, "error when trying to find documents")
	}
	defer cursor.Close(ctx)

	var data []T
	if err := cursor.All(ctx, &data); err != nil {
		return nil, errorsWrapper.Wrap(err, "error when trying to decode documents")
	}

	totalPages := int(math.Ceil(float64(total) / float64(params.PageSize)))

	return &domain_pagination.Result[T]{
		Data:       data,
		Total:      total,
		Page:       params.Page,
		PageSize:   params.PageSize,
		TotalPages: totalPages,
	}, nil
}
