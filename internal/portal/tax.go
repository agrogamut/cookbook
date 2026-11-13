package portal

// Policy selection records the owner's instruction, not professional tax approval.
type TaxPolicy struct {
	PatientPolicy    string
	WithdrawalPolicy string
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
	if p.patientPolicy() != "inactive" {
		v.TaxBPS = 1800
		v.TaxPaise = int(portion(int64(base), v.TaxBPS))
		v.TotalPaise += v.TaxPaise
		v.Policy = p.patientPolicy()
	}
	return v
}
func (p TaxPolicy) withdrawal(gross int64) (deduction int64, bps int, policy string) {
	if p.withdrawalPolicy() == "inactive" {
		return portion(gross, 1800), 1800, "pending_review"
	}
	return portion(gross, 1800), 1800, p.withdrawalPolicy()
}

func selectedPolicy(value string) string {
	if value == "" {
		return "owner-18-v1"
	}
	return value
}
func (p TaxPolicy) patientPolicy() string    { return selectedPolicy(p.PatientPolicy) }
func (p TaxPolicy) withdrawalPolicy() string { return selectedPolicy(p.WithdrawalPolicy) }
