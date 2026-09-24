package data

import (
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	paginationV1 "github.com/tx7do/go-crud/api/gen/go/pagination/v1"
	"github.com/tx7do/go-crud/pagination"
	paginationFilter "github.com/tx7do/go-crud/pagination/filter"
	"github.com/tx7do/go-crud/pagination/paginator"
	paginationSorting "github.com/tx7do/go-crud/pagination/sorting"
	q "go-wind-admin/app/admin/service/internal/data/quotasql"
)

// These are JSONPath expressions, bound as data to the fixed sqlc queries.
// Field identifiers are enumerated and all values are JSON-encoded; no SQL text
// or dynamic SQL execution is exposed by this adapter.
func planQuotaField(raw string) (string, bool) {
	for _, field := range []string{"id", "plan_id", "quota_code", "quota_type", "quota_value", "created_at", "updated_at", "deleted_at", "created_by", "updated_by", "deleted_by"} {
		if strings.ReplaceAll(strings.ToLower(raw), "_", "") == strings.ReplaceAll(field, "_", "") {
			return field, true
		}
	}
	return "", false
}
func planQuotaLiteral(field, value string) (string, error) {
	switch field {
	case "id", "plan_id", "quota_value", "created_by", "updated_by", "deleted_by":
		n, e := strconv.ParseInt(value, 10, 64)
		if e != nil {
			return "", QuotaErrInvalid("invalid numeric quota filter")
		}
		return strconv.FormatInt(n, 10), nil
	default:
		b, _ := json.Marshal(value)
		if strings.HasSuffix(field, "_at") {
			return string(b) + ".datetime()", nil
		}
		return string(b), nil
	}
}
func planQuotaCondition(c *paginationV1.FilterCondition, searches *[]map[string]string) (string, error) {
	field, ok := planQuotaField(c.GetField())
	if !ok {
		return "", QuotaErrInvalid("unknown plan quota filter field")
	}
	key := "@." + field
	switch c.GetOp() {
	case paginationV1.Operator_SEARCH:
		if strings.TrimSpace(c.GetValue()) == "" {
			return "1 == 1", nil
		}
		index := len(*searches)
		*searches = append(*searches, map[string]string{"field": field, "value": c.GetValue()})
		return "@._search[" + strconv.Itoa(index) + "] == true", nil
	case paginationV1.Operator_IS_NULL:
		return key + " == null", nil
	case paginationV1.Operator_IS_NOT_NULL:
		return key + " != null", nil
	}
	nullKey := key
	if strings.HasSuffix(field, "_at") {
		key += ".datetime()"
	}
	values := c.GetValues()
	if len(values) == 0 {
		values = []string{c.GetValue()}
	}
	if c.GetOp() == paginationV1.Operator_IN || c.GetOp() == paginationV1.Operator_NIN || c.GetOp() == paginationV1.Operator_BETWEEN {
		if len(c.GetValues()) == 0 {
			var raw []json.RawMessage
			if e := json.Unmarshal([]byte(c.GetValue()), &raw); e != nil {
				return "", QuotaErrInvalid("filter requires an array")
			}
			values = nil
			for _, v := range raw {
				var text string
				if len(v) > 0 && v[0] == '"' {
					if e := json.Unmarshal(v, &text); e != nil {
						return "", QuotaErrInvalid("invalid filter value")
					}
				} else {
					text = string(v)
				}
				values = append(values, text)
			}
		}
	}
	literals := make([]string, 0, len(values))
	for _, v := range values {
		literal, e := planQuotaLiteral(field, v)
		if e != nil {
			return "", e
		}
		literals = append(literals, literal)
	}
	if len(literals) == 0 {
		return "", QuotaErrInvalid("empty quota filter values")
	}
	compare := map[paginationV1.Operator]string{paginationV1.Operator_EQ: " == ", paginationV1.Operator_NEQ: " != ", paginationV1.Operator_EXACT: " == ", paginationV1.Operator_GT: " > ", paginationV1.Operator_GTE: " >= ", paginationV1.Operator_LT: " < ", paginationV1.Operator_LTE: " <= "}
	if op, ok := compare[c.GetOp()]; ok {
		return "(" + nullKey + " != null && " + key + op + literals[0] + ")", nil
	}
	switch c.GetOp() {
	case paginationV1.Operator_IN, paginationV1.Operator_NIN:
		parts := make([]string, 0, len(literals))
		for _, v := range literals {
			parts = append(parts, key+" == "+v)
		}
		out := "(" + strings.Join(parts, " || ") + ")"
		if c.GetOp() == paginationV1.Operator_NIN {
			out = "(" + nullKey + " != null && !(" + out + "))"
		}
		return out, nil
	case paginationV1.Operator_BETWEEN:
		if len(literals) != 2 {
			return "", QuotaErrInvalid("between requires two bounds")
		}
		return "(" + key + " >= " + literals[0] + " && " + key + " <= " + literals[1] + ")", nil
	}
	value := c.GetValue()
	flags := ""
	pattern := regexp.QuoteMeta(value)
	switch c.GetOp() {
	case paginationV1.Operator_CONTAINS:
	case paginationV1.Operator_ICONTAINS:
		flags = "i"
	case paginationV1.Operator_STARTS_WITH:
		pattern = "^" + pattern
	case paginationV1.Operator_ISTARTS_WITH:
		pattern = "^" + pattern
		flags = "i"
	case paginationV1.Operator_ENDS_WITH:
		pattern += "$"
	case paginationV1.Operator_IENDS_WITH:
		pattern += "$"
		flags = "i"
	case paginationV1.Operator_IEXACT:
		pattern = "^" + pattern + "$"
		flags = "i"
	case paginationV1.Operator_REGEXP:
		pattern = value
	case paginationV1.Operator_IREGEXP:
		pattern = value
		flags = "i"
	default:
		return "", QuotaErrInvalid("unsupported quota filter operator")
	}
	p, _ := json.Marshal(pattern)
	out := key + " like_regex " + string(p)
	if flags != "" {
		out += " flag \"" + flags + "\""
	}
	return out, nil
}
func planQuotaExpr(expr *paginationV1.FilterExpr, depth int, searches *[]map[string]string) (string, error) {
	if expr == nil {
		return "1 == 1", nil
	}
	if depth > 32 {
		return "", QuotaErrInvalid("filter nesting too deep")
	}
	op := " && "
	if expr.GetType() == paginationV1.ExprType_OR {
		op = " || "
	} else if expr.GetType() != paginationV1.ExprType_AND {
		return "", QuotaErrInvalid("unknown filter group")
	}
	parts := []string{}
	for _, c := range expr.GetConditions() {
		v, e := planQuotaCondition(c, searches)
		if e != nil {
			return "", e
		}
		parts = append(parts, "("+v+")")
	}
	for _, g := range expr.GetGroups() {
		v, e := planQuotaExpr(g, depth+1, searches)
		if e != nil {
			return "", e
		}
		parts = append(parts, "("+v+")")
	}
	if len(parts) == 0 {
		return "1 == 1", nil
	}
	return strings.Join(parts, op), nil
}

type planQuotaListQuery struct {
	q.ListPlanQuotasParams
	countPredicate string
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
	searches := []map[string]string{}
	predicate, e := planQuotaExpr(expr, 0, &searches)
	if e != nil {
		return out, e
	}
	out.Predicate = "$ ? (" + predicate + ")"
	out.countPredicate = out.Predicate
	out.SearchTerms, _ = json.Marshal(searches)
	sortings := req.GetSorting()
	if len(sortings) == 0 && req.GetOrderBy() != "" {
		sortings, e = paginationSorting.NewOrderByStringConverter().Convert(req.GetOrderBy())
		if e != nil {
			return out, QuotaErrInvalid("invalid quota order")
		}
	}
	keys := []map[string]any{}
	for _, s := range sortings {
		field, ok := planQuotaField(s.GetField())
		if !ok {
			return out, QuotaErrInvalid("unknown quota sort field")
		}
		direction := 1
		if s.GetDirection() == paginationV1.Sorting_DESC {
			direction = -1
		}
		keys = append(keys, map[string]any{"field": field, "direction": direction})
	}
	out.SortKeys, _ = json.Marshal(keys)
	if req.GetNoPaging() {
		out.PageLimit = nil
		if paginator.NoPagingMaxLimit > 0 {
			out.PageLimit = ptr(int64(paginator.NoPagingMaxLimit))
		}
		out.PageOffset = 0
		return out, nil
	}
	// Keep the original PagingRequest precedence and paired-field semantics.
	clamp := func(n int64) int64 {
		if n < 1 {
			n = 1
		}
		if paginator.MaxLimit > 0 && n > int64(paginator.MaxLimit) {
			n = int64(paginator.MaxLimit)
		}
		return n
	}
	if req.Page != nil && req.PageSize != nil {
		limit := clamp(int64(req.GetPageSize()))
		page := int64(req.GetPage())
		if page < 1 {
			page = 1
		}
		out.PageLimit = &limit
		out.PageOffset = (page - 1) * limit
	} else if req.Offset != nil && req.Limit != nil {
		if req.GetOffset() > math.MaxInt64 {
			return out, QuotaErrInvalid("quota offset overflow")
		}
		out.PageLimit = ptr(clamp(int64(req.GetLimit())))
		out.PageOffset = int64(req.GetOffset())
	} else if req.Token != nil && req.Offset != nil {
		if req.GetOffset() > math.MaxInt64 {
			return out, QuotaErrInvalid("quota page size overflow")
		}
		out.PageLimit = ptr(clamp(int64(req.GetOffset())))
		if req.GetToken() != "" {
			lastID, ok := pagination.VerifyAndDecode(req.GetToken(), pagination.TokenSecret())
			if !ok {
				return out, QuotaErrInvalid("invalid or tampered pagination token")
			}
			out.Predicate = "$ ? ((" + predicate + ") && @.id > " + strconv.FormatInt(lastID, 10) + ")"
		}
	}
	return out, nil
}
