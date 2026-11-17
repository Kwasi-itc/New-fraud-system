package sourceconnectors

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Kwasi-itc/New-fraud-system/backend/screening-service/internal/sourcedata"
)

type Reader interface {
	io.Reader
	io.ReaderAt
}
type Parsed struct {
	Total         int            `json:"total"`
	ByType        map[string]int `json:"by_type"`
	PublisherDate string         `json:"publisher_date"`
	ExpectedCount int            `json:"expected_count,omitempty"`
	Parser        string         `json:"parser"`
}
type node struct {
	name     xml.Name
	attrs    []xml.Attr
	text     string
	children []*node
}

func (n *node) attr(key string) string {
	for _, a := range n.attrs {
		if a.Name.Local == key {
			return strings.TrimSpace(a.Value)
		}
	}
	return ""
}
func (n *node) child(key string) *node {
	for _, c := range n.children {
		if c.name.Local == key {
			return c
		}
	}
	return nil
}
func (n *node) value(key string) string {
	if c := n.child(key); c != nil {
		return strings.TrimSpace(c.text)
	}
	return ""
}
func (n *node) all(key string) []*node {
	out := []*node{}
	for _, c := range n.children {
		if c.name.Local == key {
			out = append(out, c)
		}
		out = append(out, c.all(key)...)
	}
	return out
}

func readNode(d *xml.Decoder, start xml.StartElement, depth int, begin int64) (*node, error) {
	if depth > 64 || len(start.Attr) > 256 {
		return nil, ErrLimit
	}
	n := &node{name: start.Name, attrs: start.Attr}
	var text strings.Builder
	for {
		if d.InputOffset()-begin > 2*1024*1024 {
			return nil, ErrLimit
		}
		token, err := d.Token()
		if err != nil {
			return nil, ErrSchema
		}
		switch v := token.(type) {
		case xml.Directive:
			return nil, ErrSchema
		case xml.ProcInst:
			return nil, ErrSchema
		case xml.CharData:
			if len(v) > 2*1024*1024 {
				return nil, ErrLimit
			}
			text.Write(v)
		case xml.StartElement:
			if v.Name.Space != start.Name.Space {
				return nil, ErrSchema
			}
			c, e := readNode(d, v, depth+1, begin)
			if e != nil {
				return nil, e
			}
			n.children = append(n.children, c)
			if len(n.children) > 10000 {
				return nil, ErrLimit
			}
		case xml.EndElement:
			n.text = text.String()
			return n, nil
		}
	}
}

// Parse validates a complete bounded document and emits every source record.
// No DTD/entity expansion, external resources, XInclude or schema URL is loaded.
// XSD validation and live-source evidence are tracked separately from mapping.
func Parse(ctx context.Context, s Spec, input Reader, emit func(sourcedata.Record) error) (Parsed, error) {
	out := Parsed{ByType: map[string]int{}, Parser: s.Parser}
	limited := &io.LimitedReader{R: input, N: MaxArtifactBytes + 1}
	d := xml.NewDecoder(limited)
	d.Strict = true
	rootSeen, rootClosed := false, false
	metadataSeen := false
	depth := 0
	seen := map[string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		begin := d.InputOffset()
		token, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, ErrSchema
		}
		if limited.N <= 0 {
			return out, ErrLimit
		}
		switch v := token.(type) {
		case xml.Directive:
			return out, ErrSchema
		case xml.ProcInst:
			if v.Target != "xml" || rootSeen {
				return out, ErrSchema
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(v)) != "" {
				return out, ErrSchema
			}
		case xml.StartElement:
			if rootClosed {
				return out, ErrSchema
			}
			depth++
			if depth == 1 {
				if rootSeen || v.Name.Local != s.Root || v.Name.Space != s.Namespace {
					return out, ErrSchema
				}
				rootSeen = true
				if s.Key == "un-consolidated" {
					for _, a := range v.Attr {
						if a.Name.Local == "dateGenerated" {
							out.PublisherDate = a.Value
						}
					}
				}
				continue
			}
			if depth > 64 {
				return out, ErrLimit
			}
			if depth == 2 {
				allowed := (s.Key == "ofac-sdn" && (v.Name.Local == "publshInformation" || v.Name.Local == "sdnEntry")) || (s.Key == "uksl" && (v.Name.Local == "DateGenerated" || v.Name.Local == "Designation")) || (s.Key == "un-consolidated" && (v.Name.Local == "INDIVIDUALS" || v.Name.Local == "ENTITIES")) || (s.Key == "eu-fsf" && v.Name.Local == "sanctionEntity")
				if !allowed {
					return out, ErrSchema
				}
			}
			entry := (s.Key == "ofac-sdn" && v.Name.Local == "sdnEntry" && depth == 2) || (s.Key == "uksl" && v.Name.Local == "Designation" && depth == 2) || (s.Key == "un-consolidated" && (v.Name.Local == "INDIVIDUAL" || v.Name.Local == "ENTITY") && depth == 3) || (s.Key == "eu-fsf" && v.Name.Local == "sanctionEntity" && depth == 2)
			meta := (s.Key == "ofac-sdn" && v.Name.Local == "publshInformation") || (s.Key == "uksl" && v.Name.Local == "DateGenerated")
			if entry || meta {
				n, e := readNode(d, v, depth, begin)
				if e != nil {
					return out, e
				}
				depth--
				if meta {
					if metadataSeen {
						return out, ErrSchema
					}
					metadataSeen = true
					if s.Key == "ofac-sdn" {
						out.PublisherDate = n.value("Publish_Date")
						out.ExpectedCount, _ = strconv.Atoi(n.value("Record_Count"))
					} else {
						out.PublisherDate = strings.TrimSpace(n.text)
					}
					continue
				}
				record, e := mapRecord(s, n)
				if e != nil {
					return out, e
				}
				length := d.InputOffset() - begin
				if length < 1 || length > 2*1024*1024 {
					return out, ErrLimit
				}
				raw := make([]byte, int(length))
				if _, e = input.ReadAt(raw, begin); e != nil {
					return out, ErrSchema
				}
				record.OriginalXML = string(raw)
				if seen[record.ID] || record.Validate() != nil {
					return out, ErrSchema
				}
				seen[record.ID] = true
				out.Total++
				out.ByType[record.Type]++
				if out.Total > 100000 {
					return out, ErrLimit
				}
				if e = emit(record); e != nil {
					return out, e
				}
			}
		case xml.EndElement:
			depth--
			if depth == 0 {
				rootClosed = true
			}
			if depth < 0 {
				return out, ErrSchema
			}
		}
	}
	if !rootSeen || !rootClosed || depth != 0 || out.Total == 0 || out.PublisherDate == "" && s.Key != "eu-fsf" || (s.Key == "ofac-sdn" && (out.ExpectedCount == 0 || out.Total != out.ExpectedCount)) {
		return out, ErrSchema
	}
	return out, nil
}

func mapRecord(s Spec, n *node) (sourcedata.Record, error) {
	r := sourcedata.Record{Properties: map[string][]string{}, AliasQualities: map[string]string{}}
	add := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			r.Properties[key] = append(r.Properties[key], value)
		}
	}
	alias := func(value, quality string) {
		value = strings.Join(strings.Fields(value), " ")
		if value != "" && value != r.Name {
			for _, existing := range r.Aliases {
				if value == existing {
					return
				}
			}
			r.Aliases = append(r.Aliases, value)
			r.AliasQualities[value] = quality
		}
	}
	id := func(kind, value, country string) {
		value = strings.TrimSpace(value)
		if value != "" {
			r.Identifiers = append(r.Identifiers, sourcedata.Identifier{Type: identifierType(kind), Value: value, Country: CountryCode(country), Issuer: strings.TrimSpace(country), OriginalType: kind})
		}
	}
	date := func(value string) {
		if normalized := canonicalDate(value); normalized != "" {
			add("birthDate", normalized)
		} else {
			add("birthDateText", value)
		}
	}
	country := func(key, value string) {
		if code := CountryCode(value); code != "" {
			add(key, code)
		} else {
			add(key+"Text", value)
		}
	}
	switch s.Key {
	case "ofac-sdn":
		r.ID = n.value("uid")
		r.Name = join(n.value("firstName"), n.value("lastName"))
		r.SourceType = n.value("sdnType")
		r.Type = entityType(r.SourceType)
		for _, a := range n.all("aka") {
			quality := "unspecified"
			if strings.EqualFold(a.value("category"), "weak") {
				quality = "weak"
			} else if strings.EqualFold(a.value("category"), "strong") {
				quality = "strong"
			}
			alias(join(a.value("firstName"), a.value("lastName")), quality)
		}
		for _, v := range n.all("program") {
			add("programs", v.text)
		}
		for _, v := range n.all("id") {
			id(v.value("idType"), v.value("idNumber"), v.value("idCountry"))
		}
		for _, v := range n.all("dateOfBirth") {
			date(v.text)
		}
		for _, v := range n.all("nationality") {
			country("nationality", v.value("country"))
		}
		for _, v := range n.all("address") {
			add("address", join(v.value("address1"), v.value("address2"), v.value("address3"), v.value("city"), v.value("stateOrProvince"), v.value("postalCode"), v.value("country")))
			country("country", v.value("country"))
		}
	case "un-consolidated":
		r.ID = n.value("DATAID")
		r.Name = join(n.value("FIRST_NAME"), n.value("SECOND_NAME"), n.value("THIRD_NAME"), n.value("FOURTH_NAME"))
		r.SourceType = n.name.Local
		if n.name.Local == "INDIVIDUAL" {
			r.Type = "Person"
		} else {
			r.Type = "Organization"
		}
		add("programs", n.value("UN_LIST_TYPE"))
		add("listedAt", n.value("LISTED_ON"))
		for _, v := range append(n.all("INDIVIDUAL_ALIAS"), n.all("ENTITY_ALIAS")...) {
			quality := "unspecified"
			if strings.EqualFold(v.value("QUALITY"), "Good") {
				quality = "strong"
			} else if strings.EqualFold(v.value("QUALITY"), "Low") {
				quality = "weak"
			}
			alias(v.value("ALIAS_NAME"), quality)
		}
		alias(n.value("NAME_ORIGINAL_SCRIPT"), "unspecified")
		if v := n.child("NATIONALITY"); v != nil {
			for _, x := range v.all("VALUE") {
				country("nationality", x.text)
			}
		}
		for _, v := range n.all("INDIVIDUAL_DOCUMENT") {
			id(v.value("TYPE_OF_DOCUMENT"), v.value("NUMBER"), v.value("ISSUING_COUNTRY"))
		}
		for _, v := range n.all("INDIVIDUAL_DATE_OF_BIRTH") {
			value := v.value("DATE")
			if value == "" {
				value = v.value("YEAR")
			}
			if v.value("FROM_YEAR") != "" {
				value = v.value("FROM_YEAR") + " to " + v.value("TO_YEAR")
			}
			date(value)
		}
		for _, v := range append(n.all("INDIVIDUAL_ADDRESS"), n.all("ENTITY_ADDRESS")...) {
			add("address", join(v.value("STREET"), v.value("CITY"), v.value("STATE_PROVINCE"), v.value("COUNTRY")))
			country("country", v.value("COUNTRY"))
		}
	case "uksl":
		r.ID = n.value("UniqueID")
		r.SourceType = n.value("IndividualEntityShip")
		r.Type = entityType(r.SourceType)
		names := n.all("Name")
		for _, v := range names {
			value := join(v.value("Name1"), v.value("Name2"), v.value("Name3"), v.value("Name4"), v.value("Name5"), v.value("Name6"))
			if r.Name == "" && v.value("NameType") == "Primary Name" {
				r.Name = value
			}
		}
		if r.Name == "" && len(names) > 0 {
			r.Name = join(names[0].value("Name1"), names[0].value("Name2"), names[0].value("Name3"), names[0].value("Name4"), names[0].value("Name5"), names[0].value("Name6"))
		}
		for _, v := range names {
			quality := "unspecified"
			if strings.Contains(strings.ToLower(v.value("AliasStrength")), "low") {
				quality = "weak"
			}
			alias(join(v.value("Name1"), v.value("Name2"), v.value("Name3"), v.value("Name4"), v.value("Name5"), v.value("Name6")), quality)
		}
		for _, v := range n.all("NameNonLatinScript") {
			alias(v.text, "unspecified")
		}
		add("programs", n.value("RegimeName"))
		add("sanctionsImposed", n.value("SanctionsImposed"))
		add("listedAt", n.value("DateDesignated"))
		for _, v := range n.all("DOB") {
			date(v.text)
		}
		for _, v := range n.all("Nationality") {
			country("nationality", v.text)
		}
		for _, v := range n.all("Address") {
			add("address", join(v.value("AddressLine1"), v.value("AddressLine2"), v.value("AddressLine3"), v.value("AddressLine4"), v.value("AddressLine5"), v.value("AddressLine6"), v.value("AddressCountry")))
			country("country", v.value("AddressCountry"))
		}
		for _, v := range n.all("PassportNumber") {
			id("passport", v.text, "")
		}
		for _, v := range n.all("NationalIdentificationNumber") {
			id("national identification", v.text, "")
		}
		for _, v := range n.all("IMONumber") {
			id("IMO", v.text, "")
		}
	case "eu-fsf":
		r.ID = n.attr("logicalId")
		if subject := n.child("subjectType"); subject != nil {
			r.SourceType = subject.attr("code")
			if r.SourceType == "person" {
				r.Type = "Person"
			} else if r.SourceType == "enterprise" {
				r.Type = "Organization"
			}
		}
		names := n.all("nameAlias")
		for _, v := range names {
			if r.Name == "" {
				r.Name = v.attr("wholeName")
			}
			alias(v.attr("wholeName"), "unspecified")
		}
		for _, v := range n.all("regulation") {
			add("programs", v.attr("programme"))
		}
		for _, v := range n.all("birthdate") {
			value := v.attr("birthdate")
			if value == "" {
				value = v.attr("year")
			}
			date(value)
		}
		for _, v := range n.all("citizenship") {
			country("nationality", v.attr("countryIso2Code"))
		}
		for _, v := range n.all("identification") {
			id(v.attr("identificationTypeCode"), v.attr("number"), v.attr("countryIso2Code"))
		}
		for _, v := range n.all("address") {
			add("address", join(v.attr("street"), v.attr("city"), v.attr("zipCode"), v.attr("countryDescription")))
			country("country", v.attr("countryIso2Code"))
		}
	default:
		return r, ErrSource
	}
	if r.ID == "" || r.Name == "" || r.Type == "" {
		return r, ErrSchema
	}
	r.SourceReference = s.Reference + "#record=" + r.ID
	return r, nil
}
func join(values ...string) string {
	return strings.Join(strings.Fields(strings.Join(values, " ")), " ")
}
func entityType(value string) string {
	switch strings.ToLower(value) {
	case "individual", "person":
		return "Person"
	case "entity", "organization":
		return "Organization"
	case "vessel", "ship":
		return "Vessel"
	case "aircraft":
		return "Aircraft"
	}
	return ""
}
func identifierType(value string) string {
	v := strings.ToLower(value)
	switch {
	case strings.Contains(v, "passport"):
		return "passport"
	case strings.Contains(v, "national") && strings.Contains(v, "ident"):
		return "national_id"
	case strings.Contains(v, "registration"):
		return "registration"
	case strings.Contains(v, "tax") || strings.Contains(v, "vat"):
		return "tax_id"
	case strings.Contains(v, "legal entity") || v == "lei":
		return "lei"
	case strings.Contains(v, "imo"):
		return "imo"
	}
	return "other"
}
func canonicalDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 4 {
		if year, e := strconv.Atoi(value); e == nil && year > 0 && year <= time.Now().Year() {
			return value
		}
	}
	for _, layout := range []string{"2006-01-02", "02 Jan 2006", "2 Jan 2006", "02/01/2006", "2/1/2006", "2006-01-02T15:04:05"} {
		if date, e := time.Parse(layout, value); e == nil {
			return date.Format("2006-01-02")
		}
	}
	return ""
}
