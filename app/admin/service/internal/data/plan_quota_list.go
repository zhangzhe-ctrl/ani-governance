package data

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"go-wind-admin/app/admin/service/internal/data/ent/planquota"
	"go-wind-admin/app/admin/service/internal/data/ent/predicate"
	paginationV1 "go-wind-admin/pkg/localdeps/go-crud/api/gen/go/pagination/v1"
	"go-wind-admin/pkg/localdeps/go-crud/pagination"
	paginationFilter "go-wind-admin/pkg/localdeps/go-crud/pagination/filter"
	"go-wind-admin/pkg/localdeps/go-crud/pagination/paginator"
	paginationSorting "go-wind-admin/pkg/localdeps/go-crud/pagination/sorting"
)

func planQuotaField(raw string) (string, bool) {
	for _, field := range []string{"id", "plan_id", "quota_code", "quota_type", "quota_value", "created_at", "updated_at", "deleted_at", "created_by", "updated_by", "deleted_by"} {
		if strings.ReplaceAll(strings.ToLower(raw), "_", "") == strings.ReplaceAll(field, "_", "") {
			return field, true
		}
	}
	return "", false
}

func planQuotaFilterValue(field, value string) (any, error) {
	switch field {
	case "id", "plan_id", "quota_value", "created_by", "updated_by", "deleted_by":
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, QuotaErrInvalid("invalid numeric quota filter")
		}
		return v, nil
	}
	if strings.HasSuffix(field, "_at") {
		for _, format := range []string{time.RFC3339Nano, "2006-01-02"} {
			if v, err := time.Parse(format, value); err == nil {
				return v, nil
			}
		}
		return nil, QuotaErrInvalid("invalid timestamp quota filter")
	}
	return value, nil
}

type quotaFilter func(*entsql.Selector) *entsql.Predicate

func planQuotaCondition(c *paginationV1.FilterCondition) (quotaFilter, error) {
	field, ok := planQuotaField(c.GetField())
	if !ok {
		return nil, QuotaErrInvalid("unknown plan quota filter field")
	}
	op := c.GetOp()
	// SEARCH uses the historical JSON text representation for every accepted
	// field. Handle it before numeric/time comparison parsing, including blank
	// searches. #>> '{}' extracts scalar JSON text without JSON string quotes.
	if op == paginationV1.Operator_SEARCH {
		value := c.GetValue()
		return func(s *entsql.Selector) *entsql.Predicate {
			if strings.TrimSpace(value) == "" {
				return entsql.ExprP("TRUE")
			}
			return entsql.P(func(b *entsql.Builder) {
				b.WriteString("to_tsvector(coalesce(to_jsonb(").Ident(s.C(field)).WriteString(") #>> '{}', '')) @@ plainto_tsquery(").Arg(value).WriteByte(')')
			})
		}, nil
	}
	if op == paginationV1.Operator_IS_NULL {
		return func(s *entsql.Selector) *entsql.Predicate { return entsql.IsNull(s.C(field)) }, nil
	}
	if op == paginationV1.Operator_IS_NOT_NULL {
		return func(s *entsql.Selector) *entsql.Predicate { return entsql.NotNull(s.C(field)) }, nil
	}
	values := c.GetValues()
	if len(values) == 0 {
		values = []string{c.GetValue()}
	}
	if op == paginationV1.Operator_IN || op == paginationV1.Operator_NIN || op == paginationV1.Operator_BETWEEN {
		if len(c.GetValues()) == 0 {
			var raw []json.RawMessage
			if json.Unmarshal([]byte(c.GetValue()), &raw) != nil {
				return nil, QuotaErrInvalid("filter requires an array")
			}
			values = nil
			for _, v := range raw {
				var text string
				if len(v) > 0 && v[0] == '"' {
					if json.Unmarshal(v, &text) != nil {
						return nil, QuotaErrInvalid("invalid filter value")
					}
				} else {
					text = string(v)
				}
				values = append(values, text)
			}
		}
	}
	if len(values) == 0 {
		return nil, QuotaErrInvalid("empty quota filter values")
	}
	args := make([]any, 0, len(values))
	for _, value := range values {
		v, e := planQuotaFilterValue(field, value)
		if e != nil {
			return nil, e
		}
		args = append(args, v)
	}
	if op == paginationV1.Operator_BETWEEN && len(args) != 2 {
		return nil, QuotaErrInvalid("between requires two bounds")
	}
	switch op {
	case paginationV1.Operator_EQ, paginationV1.Operator_EXACT, paginationV1.Operator_NEQ, paginationV1.Operator_GT, paginationV1.Operator_GTE, paginationV1.Operator_LT, paginationV1.Operator_LTE, paginationV1.Operator_IN, paginationV1.Operator_NIN, paginationV1.Operator_BETWEEN:
		return func(s *entsql.Selector) *entsql.Predicate {
			col := s.C(field)
			switch op {
			case paginationV1.Operator_EQ, paginationV1.Operator_EXACT:
				return entsql.EQ(col, args[0])
			case paginationV1.Operator_NEQ:
				return entsql.NEQ(col, args[0])
			case paginationV1.Operator_GT:
				return entsql.GT(col, args[0])
			case paginationV1.Operator_GTE:
				return entsql.GTE(col, args[0])
			case paginationV1.Operator_LT:
				return entsql.LT(col, args[0])
			case paginationV1.Operator_LTE:
				return entsql.LTE(col, args[0])
			case paginationV1.Operator_IN:
				return entsql.In(col, args...)
			case paginationV1.Operator_NIN:
				return entsql.NotIn(col, args...)
			default:
				return entsql.And(entsql.GTE(col, args[0]), entsql.LTE(col, args[1]))
			}
		}, nil
	}
	value := c.GetValue()

	pattern := regexp.QuoteMeta(value)
	operator := " ~ "
	switch op {
	case paginationV1.Operator_CONTAINS:
	case paginationV1.Operator_ICONTAINS:
		operator = " ~* "
	case paginationV1.Operator_STARTS_WITH:
		pattern = "^" + pattern
	case paginationV1.Operator_ISTARTS_WITH:
		pattern = "^" + pattern
		operator = " ~* "
	case paginationV1.Operator_ENDS_WITH:
		pattern += "$"
	case paginationV1.Operator_IENDS_WITH:
		pattern += "$"
		operator = " ~* "
	case paginationV1.Operator_IEXACT:
		pattern = "^" + pattern + "$"
		operator = " ~* "
	case paginationV1.Operator_REGEXP:
		pattern = value
	case paginationV1.Operator_IREGEXP:
		pattern = value
		operator = " ~* "
	default:
		return nil, QuotaErrInvalid("unsupported quota filter operator")
	}
	return func(s *entsql.Selector) *entsql.Predicate {
		return entsql.P(func(b *entsql.Builder) { b.Ident(s.C(field)).WriteString(operator).Arg(pattern) })
	}, nil
}

func planQuotaExpr(expr *paginationV1.FilterExpr, depth int) (quotaFilter, error) {
	if expr == nil {
		return func(*entsql.Selector) *entsql.Predicate { return entsql.ExprP("TRUE") }, nil
	}
	if depth > 32 {
		return nil, QuotaErrInvalid("filter nesting too deep")
	}
	if expr.GetType() != paginationV1.ExprType_AND && expr.GetType() != paginationV1.ExprType_OR {
		return nil, QuotaErrInvalid("unknown filter group")
	}
	var parts []quotaFilter
	for _, c := range expr.GetConditions() {
		p, e := planQuotaCondition(c)
		if e != nil {
			return nil, e
		}
		parts = append(parts, p)
	}
	for _, g := range expr.GetGroups() {
		p, e := planQuotaExpr(g, depth+1)
		if e != nil {
			return nil, e
		}
		parts = append(parts, p)
	}
	return func(s *entsql.Selector) *entsql.Predicate {
		if len(parts) == 0 {
			return entsql.ExprP("TRUE")
		}
		ps := make([]*entsql.Predicate, 0, len(parts))
		for _, p := range parts {
			ps = append(ps, p(s))
		}
		if expr.GetType() == paginationV1.ExprType_OR {
			return entsql.Or(ps...)
		}
		return entsql.And(ps...)
	}, nil
}

type planQuotaListQuery struct {
	filter  predicate.PlanQuota
	order   []planquota.OrderOption
	limit   *int
	offset  int
	afterID *uint32
}

func planQuotaListParams(req *paginationV1.PagingRequest) (planQuotaListQuery, error) {
	var out planQuotaListQuery
	expr := req.GetFilterExpr()
	var e error
	if req.GetQuery() != "" {
		expr, e = paginationFilter.NewQueryStringConverter().Convert(req.GetQuery())
	} else if req.GetFilter() != "" {
		expr, e = paginationFilter.NewFilterStringConverter().Convert(req.GetFilter())
	}
	if e != nil {
		return out, QuotaErrInvalid("invalid quota filter")
	}
	filter, e := planQuotaExpr(expr, 0)
	if e != nil {
		return out, e
	}
	out.filter = func(s *entsql.Selector) { s.Where(filter(s)) }
	sortings := req.GetSorting()
	if len(sortings) == 0 && req.GetOrderBy() != "" {
		sortings, e = paginationSorting.NewOrderByStringConverter().Convert(req.GetOrderBy())
		if e != nil {
			return out, QuotaErrInvalid("invalid quota order")
		}
	}
	for _, order := range sortings {
		field, ok := planQuotaField(order.GetField())
		if !ok {
			return out, QuotaErrInvalid("unknown quota sort field")
		}
		desc := order.GetDirection() == paginationV1.Sorting_DESC
		out.order = append(out.order, func(s *entsql.Selector) {
			if desc {
				s.OrderBy(entsql.Desc(s.C(field)))
			} else {
				s.OrderBy(s.C(field))
			}
		})
	}
	out.order = append(out.order, planquota.ByID())
	if req.GetNoPaging() {
		if paginator.NoPagingMaxLimit > 0 {
			out.limit = ptr(int(paginator.NoPagingMaxLimit))
		}
		return out, nil
	}
	clamp := func(n int64) int {
		if n < 1 {
			n = 1
		}
		if paginator.MaxLimit > 0 && n > int64(paginator.MaxLimit) {
			n = int64(paginator.MaxLimit)
		}
		return int(n)
	}
	if req.Page != nil && req.PageSize != nil {
		limit := clamp(int64(req.GetPageSize()))
		page := max(1, int64(req.GetPage()))
		out.limit = &limit
		out.offset = int(page-1) * limit
	} else if req.Offset != nil && req.Limit != nil {
		if req.GetOffset() > math.MaxInt64 {
			return out, QuotaErrInvalid("quota offset overflow")
		}
		out.limit = ptr(clamp(int64(req.GetLimit())))
		out.offset = int(req.GetOffset())
	} else if req.Token != nil && req.Offset != nil {
		if req.GetOffset() > math.MaxInt64 {
			return out, QuotaErrInvalid("quota page size overflow")
		}
		out.limit = ptr(clamp(int64(req.GetOffset())))
		if req.GetToken() != "" {
			lastID, ok := pagination.VerifyAndDecode(req.GetToken(), pagination.TokenSecret())
			if !ok || lastID < 0 || lastID > math.MaxUint32 {
				return out, QuotaErrInvalid("invalid or tampered pagination token")
			}
			out.afterID = ptr(uint32(lastID))
		}
	}
	return out, nil
}
