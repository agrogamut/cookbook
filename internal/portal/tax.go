package portal

// Approval references are deployment configuration, never a browser-controlled switch.
type TaxPolicy struct {
	PatientApproval    string
	WithdrawalApproval string
}

type Price struct {
	BasePaise  int    `json:"base_paise"`
	TaxPaise   int    `json:"tax_paise"`
	TotalPaise int    `json:"total_paise"`
	TaxBPS     int    `json:"tax_bps"`
	Policy     string `json:"policy"`
}

func portion(paise int64, bps int) int64 {
	return (paise*int64(bps) + 5000) / 10000
}
func (p TaxPolicy) price(base int) Price {
	v := Price{BasePaise: base, TotalPaise: base, Policy: "inactive"}
	if p.PatientApproval != "" {
		v.TaxBPS = 1800
		v.TaxPaise = int(portion(int64(base), v.TaxBPS))
		v.TotalPaise += v.TaxPaise
		v.Policy = p.PatientApproval
	}
	return v
}
func (p TaxPolicy) withdrawal(gross int64) (deduction int64, bps int, policy string) {
	if p.WithdrawalApproval == "" {
		return portion(gross, 1800), 1800, "pending_review"
	}
	return portion(gross, 1800), 1800, p.WithdrawalApproval
}
