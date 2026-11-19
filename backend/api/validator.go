package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/mohanraj00/aavanam/backend/core/canon"
	"github.com/mohanraj00/aavanam/backend/core/jsontext"
	"github.com/mohanraj00/aavanam/backend/core/ops"
)

// DefaultValidatorDeadline is the deadline of a validator with no configured value (PUB-RCP-4).
const DefaultValidatorDeadline = 10 * time.Second

// answerLimit is the largest answer of a validator that the service reads.
const answerLimit = 1 << 16

// HTTPValidator returns the validator of one acceptance policy that asks an HTTP address of the host
// application (PUB-RCP-1). The address is service configuration. The request is a POST with one JSON object:
// `policy`, `candidate`, `receipt` and `activation`, which is null if the request of `publish` has no
// activation (PUB-RCP-2). The answer has the HTTP status 200 and one JSON object with the field `accepted`,
// true or false.
//
// The validator decides. If it gives no such answer before the deadline, the store has no decision
// (PUB-RCP-4): a failed connection, a different HTTP status, a redirect and a body of a different form are
// each no answer, never an acceptance and never a refusal.
func HTTPValidator(policy, address string, deadline time.Duration) (ops.Validator, error) {
	target, err := url.Parse(address)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
		return nil, errors.New("the address of a validator is an http or https URL with a host and no user part")
	}
	if deadline <= 0 {
		deadline = DefaultValidatorDeadline
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, candidate, receipt, activation string) ops.Decision {
		reference := jsontext.Value{}
		if activation != "" {
			reference = jsontext.S(activation)
		}
		body := canon.Canonical(jsontext.O(jsontext.M("policy", jsontext.S(policy)), jsontext.M("candidate", jsontext.S(candidate)),
			jsontext.M("receipt", jsontext.S(receipt)), jsontext.M("activation", reference)))
		ctx, cancel := context.WithTimeout(ctx, deadline)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
		if err != nil {
			return ops.NoAnswer
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return ops.NoAnswer
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return ops.NoAnswer
		}
		text, err := io.ReadAll(io.LimitReader(response.Body, answerLimit+1))
		if err != nil || len(text) > answerLimit {
			return ops.NoAnswer
		}
		answer, formError := jsontext.Parse(text, jsontext.Strict)
		accepted, has := answer.Get("accepted")
		switch {
		case formError != nil || answer.Kind != jsontext.Object || !has || accepted.Kind != jsontext.Bool:
			return ops.NoAnswer
		case accepted.Bool:
			return ops.Accepted
		}
		return ops.Rejected
	}, nil
}
