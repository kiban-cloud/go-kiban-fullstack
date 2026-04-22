package pagination

type Params struct {
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Sort     map[string]int `json:"sort"`
	Filters  map[string]any `json:"filters"`
}

type Result[T any] struct {
	Data       []T   `json:"data"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int   `json:"total_pages"`
}

func (p *Params) Validate() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 || p.PageSize > 100 {
		p.PageSize = 20
	}
}

func (p *Params) GetSkip() int64 {
	return int64((p.Page - 1) * p.PageSize)
}

func (p *Params) GetLimit() int64 {
	return int64(p.PageSize)
}
