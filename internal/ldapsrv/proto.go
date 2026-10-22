package ldapsrv

import (
	"strings"

	ber "github.com/go-asn1-ber/asn1-ber"
)

// LDAP application tags (RFC 4511 §4.2 ff.).
const (
	opBindRequest     = 0
	opBindResponse    = 1
	opUnbindRequest   = 2
	opSearchRequest   = 3
	opSearchEntry     = 4
	opSearchDone      = 5
	opModifyRequest   = 6
	opAddRequest      = 8
	opDelRequest      = 10
	opModDNRequest    = 12
	opCompareRequest  = 14
	opAbandonRequest  = 16
	opExtendedRequest = 23
)

// Result codes used here.
const (
	resultSuccess            = 0
	resultOperationsError    = 1
	resultProtocolError      = 2
	resultAuthMethodNotSupp  = 7
	resultNoSuchObject       = 32
	resultInvalidCredentials = 49
	resultInsufficientAccess = 50
	resultBusy               = 51
	resultUnavailable        = 52
	resultUnwilling          = 53
	resultOther              = 80
)

func envelope(id int64) *ber.Packet {
	p := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "LDAP Response")
	p.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, "MessageID"))
	return p
}

// result builds an LDAPResult-shaped response for op.
func result(id int64, op ber.Tag, code int, diag string) []byte {
	p := envelope(id)
	r := ber.Encode(ber.ClassApplication, ber.TypeConstructed, op, nil, "Response")
	r.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, int64(code), "resultCode"))
	r.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", "matchedDN"))
	r.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, diag, "diagnosticMessage"))
	p.AppendChild(r)
	return p.Bytes()
}

// entry is one directory object; attribute names are kept as given and
// matched case-insensitively.
type entry struct {
	DN    string
	Attrs map[string][]string
}

func (e entry) get(name string) []string {
	for k, v := range e.Attrs {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

// encodeEntry builds a SearchResultEntry with the requested attributes
// (all when none, "*" included, are requested).
func encodeEntry(id int64, e entry, want []string, typesOnly bool) []byte {
	p := envelope(id)
	r := ber.Encode(ber.ClassApplication, ber.TypeConstructed, opSearchEntry, nil, "SearchResultEntry")
	r.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, e.DN, "objectName"))
	attrs := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "attributes")
	all := len(want) == 0
	for _, w := range want {
		if w == "*" {
			all = true
		}
	}
	for name, vals := range e.Attrs {
		if !all && !containsFold(want, name) {
			continue
		}
		a := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "attribute")
		a.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, name, "type"))
		set := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "vals")
		if !typesOnly {
			for _, v := range vals {
				set.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, v, "value"))
			}
		}
		a.AppendChild(set)
		attrs.AppendChild(a)
	}
	r.AppendChild(attrs)
	p.AppendChild(r)
	return p.Bytes()
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// str returns a primitive packet's content as a string.
func str(p *ber.Packet) string {
	if p == nil || p.Data == nil {
		return ""
	}
	return p.Data.String()
}

// num returns a primitive INTEGER/ENUMERATED/BOOLEAN value.
func num(p *ber.Packet) int64 {
	switch v := p.Value.(type) {
	case int64:
		return v
	case bool:
		if v {
			return 1
		}
	}
	return 0
}
