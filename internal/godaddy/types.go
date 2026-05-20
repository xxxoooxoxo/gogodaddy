package godaddy

import "fmt"

var validRecordTypes = map[string]bool{
	"A":     true,
	"AAAA":  true,
	"CNAME": true,
	"MX":    true,
	"NS":    true,
	"SOA":   true,
	"SRV":   true,
	"TXT":   true,
}

type DNSRecord struct {
	Type     string `json:"type,omitempty"`
	Name     string `json:"name,omitempty"`
	Data     string `json:"data"`
	TTL      *int   `json:"ttl,omitempty"`
	Priority *int   `json:"priority,omitempty"`
	Port     *int   `json:"port,omitempty"`
	Weight   *int   `json:"weight,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Service  string `json:"service,omitempty"`
}

func ValidateRecord(record DNSRecord, requireTypeName bool) error {
	if requireTypeName {
		if record.Type == "" {
			return fmt.Errorf("record type is required")
		}
		if !validRecordTypes[record.Type] {
			return fmt.Errorf("unsupported record type %q", record.Type)
		}
		if record.Name == "" {
			return fmt.Errorf("record name is required")
		}
	}
	if record.Data == "" {
		return fmt.Errorf("record data is required")
	}
	if err := validatePositive("ttl", record.TTL); err != nil {
		return err
	}
	if err := validatePositive("priority", record.Priority); err != nil {
		return err
	}
	if err := validatePositive("weight", record.Weight); err != nil {
		return err
	}
	if record.Port != nil && (*record.Port < 1 || *record.Port > 65535) {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func ValidateTypeName(recordType, name string) error {
	if recordType == "" {
		return fmt.Errorf("record type is required")
	}
	if !validRecordTypes[recordType] {
		return fmt.Errorf("unsupported record type %q", recordType)
	}
	if name == "" {
		return fmt.Errorf("record name is required")
	}
	return nil
}

func validatePositive(name string, value *int) error {
	if value != nil && *value < 1 {
		return fmt.Errorf("%s must be positive", name)
	}
	return nil
}
