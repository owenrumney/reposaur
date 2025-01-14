package builtins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
	"github.com/open-policy-agent/opa/types"
)

var GitHubRequestBuiltin = rego.Function{
	Name: "github.request",
	Decl: types.NewFunction(
		types.Args(
			types.S,
			types.NewObject(nil, types.NewDynamicProperty(types.S, types.A)),
		),
		types.A,
	),
	Memoize: true,
}

func GitHubRequestBuiltinImpl(client *http.Client) func(bctx rego.BuiltinContext, op1, op2 *ast.Term) (*ast.Term, error) {
	return func(bctx rego.BuiltinContext, op1, op2 *ast.Term) (*ast.Term, error) {
		var unparsedReq string
		var data map[string]interface{}

		if err := ast.As(op1.Value, &unparsedReq); err != nil {
			return nil, err
		} else if err := ast.As(op2.Value, &data); err != nil {
			return nil, err
		}

		reqSlice := strings.Split(unparsedReq, " ")
		method := reqSlice[0]
		path := reqSlice[1]

		pathParams := parsePathParams(path)

		for _, p := range pathParams {
			v, err := parseValueToString(data[p])
			if err != nil {
				return nil, err
			}

			path = strings.Replace(path, "{"+p+"}", v, 1)
			delete(data, p)
		}

		u, err := url.Parse(path)
		if err != nil {
			return nil, err
		}

		qs := u.Query()
		method = strings.ToUpper(method)

		if method == http.MethodGet || method == http.MethodPost {
			for k, v := range data {
				v, err := parseValueToString(v)
				if err != nil {
					return nil, err
				}

				qs.Add(k, v)
				delete(data, k)
			}
		}

		u.RawQuery = qs.Encode()

		buf := &bytes.Buffer{}
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(data); err != nil {
			return nil, err
		}

		req, err := http.NewRequest(method, u.String(), buf)
		if err != nil {
			return nil, err
		}

		req.Header.Set("User-Agent", "reposaur")
		req.Header.Set("Content-Type", "application/json")

		finalResp := GitHubResponse{}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&finalResp.Body); err != nil {
			return nil, err
		}

		finalResp.StatusCode = resp.StatusCode

		if finalResp.StatusCode == http.StatusForbidden {
			b := finalResp.Body.(map[string]interface{})
			return nil, fmt.Errorf("forbidden: %s", b["message"])
		}

		val, err := ast.InterfaceToValue(finalResp)
		if err != nil {
			return nil, err
		}

		return ast.NewTerm(val), nil
	}
}

func parseValueToString(v interface{}) (string, error) {
	switch tv := v.(type) {
	case string:
		return tv, nil

	case json.Number:
		return tv.String(), nil

	case int64:
		return strconv.Itoa(int(tv)), nil
	}

	return "", fmt.Errorf("parse error: can't parse '%v' to string", v)
}

func parsePathParams(path string) []string {
	regex := regexp.MustCompile(`{[a-z]+}`)
	matches := regex.FindAllString(path, -1)

	var params []string
	for _, v := range matches {
		p := strings.Replace(v, "{", "", 1)
		p = strings.Replace(p, "}", "", 1)
		params = append(params, p)
	}

	return params
}
