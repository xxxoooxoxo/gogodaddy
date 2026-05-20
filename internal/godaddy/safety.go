package godaddy

import "fmt"

type DeleteSelector struct {
	Data     string
	TTL      *int
	Priority *int
	Port     *int
	Weight   *int
	Protocol *string
	Service  *string
}

type DeletePlan struct {
	Record          DNSRecord
	Remaining       []DNSRecord
	UseDirectDelete bool
}

func PlanDeleteOne(existing []DNSRecord, selector DeleteSelector) (DeletePlan, error) {
	if selector.Data == "" {
		return DeletePlan{}, fmt.Errorf("delete requires --data so one record can be selected")
	}
	if len(existing) == 0 {
		return DeletePlan{}, fmt.Errorf("no records exist for that type and name")
	}

	matches := make([]int, 0, 1)
	for i, record := range existing {
		if selector.matches(record) {
			matches = append(matches, i)
		}
	}

	switch len(matches) {
	case 0:
		return DeletePlan{}, fmt.Errorf("no existing record matched the delete selector")
	case 1:
	default:
		return DeletePlan{}, fmt.Errorf("%d records matched; add selectors like --ttl, --priority, --port, --weight, --protocol, or --service until exactly one record matches", len(matches))
	}

	matchIndex := matches[0]
	remaining := make([]DNSRecord, 0, len(existing)-1)
	for i, record := range existing {
		if i == matchIndex {
			continue
		}
		remaining = append(remaining, record)
	}

	return DeletePlan{
		Record:          existing[matchIndex],
		Remaining:       remaining,
		UseDirectDelete: len(existing) == 1,
	}, nil
}

func (s DeleteSelector) matches(record DNSRecord) bool {
	if record.Data != s.Data {
		return false
	}
	if !intMatches(record.TTL, s.TTL) {
		return false
	}
	if !intMatches(record.Priority, s.Priority) {
		return false
	}
	if !intMatches(record.Port, s.Port) {
		return false
	}
	if !intMatches(record.Weight, s.Weight) {
		return false
	}
	if !stringMatches(record.Protocol, s.Protocol) {
		return false
	}
	if !stringMatches(record.Service, s.Service) {
		return false
	}
	return true
}

func intMatches(recordValue, selectorValue *int) bool {
	if selectorValue == nil {
		return true
	}
	return recordValue != nil && *recordValue == *selectorValue
}

func stringMatches(recordValue string, selectorValue *string) bool {
	if selectorValue == nil {
		return true
	}
	return recordValue == *selectorValue
}
