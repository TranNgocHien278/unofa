package web_test

import (
	"net/http"
	"testing"
)

// TestLoanProducts_ConfigDriven covers GET /api/loans/products reading terms from config.
func TestLoanProducts_ConfigDriven(t *testing.T) {
	e := newEnv(t)
	ck, _ := e.register("alice")
	r := e.api(http.MethodGet, "/api/loans/products", ck, nil)
	if r.status != http.StatusOK {
		t.Fatalf("status %d body %s", r.status, r.body)
	}
	m := r.json(t)
	terms, _ := m["terms"].([]any)
	if len(terms) != 3 {
		t.Fatalf("terms = %v want the three configured terms", terms)
	}
	tm := terms[0].(map[string]any)
	if tm["term"] != "24h" || num(t, tm, "rate_bps") != 100 {
		t.Errorf("term = %v want 24h/100bps", tm)
	}
	if num(t, m, "overdue_bps") != 500 || num(t, m, "cap_multiplier") != 3 {
		t.Errorf("overdue/cap = %v/%v", m["overdue_bps"], m["cap_multiplier"])
	}
}

// TestLoans_BorrowActiveRepay covers POST /api/loans 201, GET /api/loans/active, and repay.
func TestLoans_BorrowActiveRepay(t *testing.T) {
	e := newEnv(t)
	ck, uid := e.register("alice")

	b := e.api(http.MethodPost, "/api/loans", ck, map[string]any{"principal": 500, "term": "24h"})
	if b.status != http.StatusCreated {
		t.Fatalf("borrow status %d body %s", b.status, b.body)
	}
	bm := b.json(t)
	loanID := num(t, bm, "loan_id")
	if num(t, bm, "balance_after") != 700 {
		t.Errorf("balance_after = %v want 700", bm["balance_after"])
	}
	if db := e.balance(uid); db != 700 {
		t.Errorf("DB balance %d want 700", db)
	}

	a := e.api(http.MethodGet, "/api/loans/active", ck, nil)
	if a.status != http.StatusOK {
		t.Fatalf("active status %d body %s", a.status, a.body)
	}
	if num(t, a.json(t), "loan_id") != loanID {
		t.Errorf("active loan_id mismatch")
	}

	rp := e.api(http.MethodPost, "/api/loans/"+fmtInt(loanID)+"/repay", ck, nil)
	if rp.status != http.StatusOK {
		t.Fatalf("repay status %d body %s", rp.status, rp.body)
	}
	if num(t, rp.json(t), "owed_paid") < 500 {
		t.Errorf("owed_paid = %v want at least principal 500", rp.json(t)["owed_paid"])
	}
}

// TestLoans_RepayUnknownLoanNotFound covers 404 LOAN_NOT_FOUND.
func TestLoans_RepayUnknownLoanNotFound(t *testing.T) {
	e := newEnv(t)
	ck, _ := e.register("alice")
	r := e.api(http.MethodPost, "/api/loans/999999/repay", ck, nil)
	if r.status != http.StatusNotFound || r.errCode(t) != "LOAN_NOT_FOUND" {
		t.Fatalf("status %d code %q want 404 LOAN_NOT_FOUND", r.status, r.errCode(t))
	}
}
