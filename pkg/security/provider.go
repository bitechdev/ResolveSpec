package security

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/bitechdev/ResolveSpec/pkg/logger"
	"github.com/bitechdev/ResolveSpec/pkg/reflection"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type ColumnSecurity struct {
	Schema       string            `json:"schema"`
	Tablename    string            `json:"tablename"`
	Path         []string          `json:"path"`
	ExtraFilters map[string]string `json:"extra_filters"`
	UserID       int               `json:"user_id"`
	Accesstype   string            `json:"accesstype"`
	MaskStart    int               `json:"mask_start"`
	MaskEnd      int               `json:"mask_end"`
	MaskInvert   bool              `json:"mask_invert"`
	MaskChar     string            `json:"mask_char"`
	Control      string            `json:"control"`
	ID           int               `json:"id"`
}

type RowSecurity struct {
	Schema    string `json:"schema"`
	Tablename string `json:"tablename"`
	Template  string `json:"template"`
	HasBlock  bool   `json:"has_block"`
	// UserID is the opaque user reference the security rules were loaded for.
	// It may be an int, a string/UUID, or a *UserContext, depending on what the
	// RowSecurityProvider/SecurityContext.GetUserRef implementation returns.
	UserID any `json:"user_id"`
}

func (m *RowSecurity) GetTemplate(pPrimaryKeyName string, pModelType reflect.Type) string {
	str := m.Template
	str = strings.ReplaceAll(str, "{PrimaryKeyName}", pPrimaryKeyName)
	str = strings.ReplaceAll(str, "{TableName}", m.Tablename)
	str = strings.ReplaceAll(str, "{SchemaName}", m.Schema)
	str = strings.ReplaceAll(str, "{UserID}", fmt.Sprintf("%v", m.UserID))
	return str
}

// SecurityList manages security state and caching
// It wraps a SecurityProvider and provides caching and utility methods
type SecurityList struct {
	provider SecurityProvider

	ColumnSecurityMutex sync.RWMutex
	ColumnSecurity      map[string][]ColumnSecurity
	RowSecurityMutex    sync.RWMutex
	RowSecurity         map[string]RowSecurity

	// Expiry bookkeeping for the two caches above; guarded by the same mutexes.
	colSecExpiry map[string]time.Time
	rowSecExpiry map[string]time.Time
	lastColPrune time.Time
	lastRowPrune time.Time
}

const (
	// securityCacheTTL is how long loaded rules are served without re-querying
	// the provider. Revoked rules take up to this long to take effect.
	securityCacheTTL = 30 * time.Second
	// securityLoadTimeout bounds a single provider call.
	securityLoadTimeout = 10 * time.Second
	// securityPruneGrace keeps expired entries around long enough that a
	// request that loaded them can still read them.
	securityPruneGrace = securityCacheTTL
)

// NewSecurityList creates a new security list with the given provider
func NewSecurityList(provider SecurityProvider) (*SecurityList, error) {
	if provider == nil {
		return nil, fmt.Errorf("security provider cannot be nil")
	}

	return &SecurityList{
		provider:       provider,
		ColumnSecurity: make(map[string][]ColumnSecurity),
		RowSecurity:    make(map[string]RowSecurity),
	}, nil
}

// Provider returns the underlying security provider
func (m *SecurityList) Provider() SecurityProvider {
	return m.provider
}

type CONTEXT_KEY string

const SECURITY_CONTEXT_KEY CONTEXT_KEY = "SecurityList"

func maskString(pString string, maskStart, maskEnd int, maskChar string, invert bool) string {
	strLen := len(pString)
	middleIndex := (strLen / 2)
	var newStr strings.Builder
	newStr.Grow(strLen)
	if maskStart == 0 && maskEnd == 0 {
		maskStart = strLen
		maskEnd = strLen
	}
	if maskEnd > strLen {
		maskEnd = strLen
	}
	if maskStart > strLen {
		maskStart = strLen
	}
	if maskChar == "" {
		maskChar = "*"
	}
	for index, char := range pString {
		if invert && index >= middleIndex-maskStart && index <= middleIndex {
			newStr.WriteString(maskChar)
			continue
		}
		if invert && index <= middleIndex+maskEnd && index >= middleIndex {
			newStr.WriteString(maskChar)
			continue
		}
		if !invert && index <= maskStart {
			newStr.WriteString(maskChar)
			continue
		}
		if !invert && index >= strLen-1-maskEnd {
			newStr.WriteString(maskChar)
			continue
		}
		newStr.WriteRune(char)
	}

	return newStr.String()
}

func (m *SecurityList) ColumSecurityApplyOnRecord(prevRecord reflect.Value, newRecord reflect.Value, modelType reflect.Type, pUserID int, pSchema, pTablename string) ([]string, error) {
	cols := make([]string, 0)

	if prevRecord.Type() != newRecord.Type() {
		logger.Error("prev:%s and new:%s record type mismatch", prevRecord.Type(), newRecord.Type())
		return cols, fmt.Errorf("prev and new record type mismatch")
	}

	m.ColumnSecurityMutex.RLock()
	defer m.ColumnSecurityMutex.RUnlock()

	if m.ColumnSecurity == nil {
		return cols, fmt.Errorf("security not initialized")
	}

	colsecList, ok := m.ColumnSecurity[fmt.Sprintf("%s.%s@%d", pSchema, pTablename, pUserID)]
	if !ok || colsecList == nil {
		return cols, fmt.Errorf("no column security data")
	}

	for i := range colsecList {
		colsec := &colsecList[i]
		if !strings.EqualFold(colsec.Accesstype, "mask") && !strings.EqualFold(colsec.Accesstype, "hide") {
			continue
		}
		lastRecords := interateStruct(prevRecord)
		newRecords := interateStruct(newRecord)
		var lastLoopField, lastLoopNewField reflect.Value
		pathLen := len(colsec.Path)
		for i, path := range colsec.Path {
			var nameType, fieldName string
			if len(newRecords) == 0 {
				if lastLoopNewField.IsValid() && lastLoopField.IsValid() && i < pathLen-1 {
					lastLoopNewField.Set(lastLoopField)
				}
				break
			}

			for ri := range newRecords {
				if !newRecords[ri].IsValid() || !lastRecords[ri].IsValid() {
					break
				}
				var field, oldField reflect.Value

				columnData := reflection.GetModelColumnDetail(newRecords[ri])
				lastColumnData := reflection.GetModelColumnDetail(lastRecords[ri])
				for i, cols := range columnData {
					if cols.SQLName != "" && strings.EqualFold(cols.SQLName, path) {
						nameType = "sql"
						fieldName = cols.SQLName
						field = cols.FieldValue
						oldField = lastColumnData[i].FieldValue
						break
					}
					if cols.Name != "" && strings.EqualFold(cols.Name, path) {
						nameType = "struct"
						fieldName = cols.Name
						field = cols.FieldValue
						oldField = lastColumnData[i].FieldValue
						break
					}
				}

				if !field.IsValid() || !oldField.IsValid() {
					break
				}
				lastLoopField = oldField
				lastLoopNewField = field

				if i == pathLen-1 {
					if strings.Contains(strings.ToLower(fieldName), "json") {
						prevSrc := oldField.Bytes()
						newSrc := field.Bytes()
						pathstr := strings.Join(colsec.Path, ".")
						prevPathValue := gjson.GetBytes(prevSrc, pathstr)
						newBytes, err := sjson.SetBytes(newSrc, pathstr, prevPathValue.Str)
						if err == nil {
							if field.CanSet() {
								field.SetBytes(newBytes)
							} else {
								logger.Warn("Value not settable: %v", field)
								cols = append(cols, pathstr)
							}
						}
						break
					}

					if nameType == "sql" {
						if strings.EqualFold(colsec.Accesstype, "mask") || strings.EqualFold(colsec.Accesstype, "hide") {
							field.Set(oldField)
							cols = append(cols, strings.Join(colsec.Path, "."))
						}
					}
					break
				}

				lastRecords = interateStruct(field)
				newRecords = interateStruct(oldField)
			}
		}
	}

	return cols, nil
}

func interateStruct(val reflect.Value) []reflect.Value {
	list := make([]reflect.Value, 0)

	switch val.Kind() {
	case reflect.Pointer, reflect.Interface:
		elem := val.Elem()
		if elem.IsValid() {
			list = append(list, interateStruct(elem)...)
		}
		return list
	case reflect.Array, reflect.Slice:
		for i := 0; i < val.Len(); i++ {
			elem := val.Index(i)
			if !elem.IsValid() {
				continue
			}
			list = append(list, interateStruct(elem)...)
		}
		return list
	case reflect.Struct:
		list = append(list, val)
		return list
	default:
		return list
	}
}

func setColSecValue(fieldsrc reflect.Value, colsec ColumnSecurity, fieldTypeName string) (int, reflect.Value) {
	fieldval := fieldsrc
	if fieldsrc.Kind() == reflect.Pointer || fieldsrc.Kind() == reflect.Interface {
		fieldval = fieldval.Elem()
	}

	fieldKindLower := strings.ToLower(fieldval.Kind().String())
	switch {
	case strings.Contains(fieldKindLower, "int") &&
		(strings.EqualFold(colsec.Accesstype, "mask") || strings.EqualFold(colsec.Accesstype, "hide")):
		if fieldval.CanInt() && fieldval.CanSet() {
			fieldval.SetInt(0)
		}
	case (strings.Contains(fieldKindLower, "time") || strings.Contains(fieldKindLower, "date")) &&
		(strings.EqualFold(colsec.Accesstype, "mask") || strings.EqualFold(colsec.Accesstype, "hide")):
		fieldval.SetZero()
	case strings.Contains(fieldKindLower, "string"):
		strVal := fieldval.String()
		if strings.EqualFold(colsec.Accesstype, "mask") {
			fieldval.SetString(maskString(strVal, colsec.MaskStart, colsec.MaskEnd, colsec.MaskChar, colsec.MaskInvert))
		} else if strings.EqualFold(colsec.Accesstype, "hide") {
			fieldval.SetString("")
		}
	case strings.Contains(fieldTypeName, "json") &&
		(strings.EqualFold(colsec.Accesstype, "mask") || strings.EqualFold(colsec.Accesstype, "hide")):
		if len(colsec.Path) < 2 {
			return 1, fieldval
		}
		pathstr := strings.Join(colsec.Path, ".")
		src := fieldval.Bytes()
		pathValue := gjson.GetBytes(src, pathstr)
		strValue := pathValue.String()
		if strings.EqualFold(colsec.Accesstype, "mask") {
			strValue = maskString(strValue, colsec.MaskStart, colsec.MaskEnd, colsec.MaskChar, colsec.MaskInvert)
		} else if strings.EqualFold(colsec.Accesstype, "hide") {
			strValue = ""
		}
		newBytes, err := sjson.SetBytes(src, pathstr, strValue)
		if err == nil {
			fieldval.SetBytes(newBytes)
		}
	}
	return 0, fieldsrc
}

func (m *SecurityList) ApplyColumnSecurity(records reflect.Value, modelType reflect.Type, pUserID int, pSchema, pTablename string) (reflect.Value, error) {
	defer logger.CatchPanic("ApplyColumnSecurity")()

	m.ColumnSecurityMutex.RLock()
	defer m.ColumnSecurityMutex.RUnlock()

	if m.ColumnSecurity == nil {
		return records, fmt.Errorf("security not initialized")
	}

	colsecList, ok := m.ColumnSecurity[fmt.Sprintf("%s.%s@%d", pSchema, pTablename, pUserID)]
	if !ok || colsecList == nil {
		return records, fmt.Errorf("nocolumn security data")
	}

	for i := range colsecList {
		colsec := &colsecList[i]
		if !strings.EqualFold(colsec.Accesstype, "mask") && !strings.EqualFold(colsec.Accesstype, "hide") {
			continue
		}

		if records.Kind() == reflect.Array || records.Kind() == reflect.Slice {
			for i := 0; i < records.Len(); i++ {
				record := records.Index(i)
				if !record.IsValid() {
					continue
				}

				lastRecord := interateStruct(record)
				pathLen := len(colsec.Path)
				for i, path := range colsec.Path {
					var field reflect.Value
					var nameType, fieldName string
					if len(lastRecord) == 0 {
						break
					}
					columnData := reflection.GetModelColumnDetail(lastRecord[0])
					for _, cols := range columnData {
						if cols.SQLName != "" && strings.EqualFold(cols.SQLName, path) {
							nameType = "sql"
							fieldName = cols.SQLName
							field = cols.FieldValue
							break
						}
						if cols.Name != "" && strings.EqualFold(cols.Name, path) {
							nameType = "struct"
							fieldName = cols.Name
							field = cols.FieldValue
							break
						}
					}

					if i == pathLen-1 {
						if nameType == "sql" || nameType == "struct" {
							setColSecValue(field, *colsec, fieldName)
						}
						break
					}
					if field.IsValid() {
						lastRecord = interateStruct(field)
					}
				}
			}
		}
	}

	return records, nil
}

func (m *SecurityList) LoadColumnSecurity(ctx context.Context, pUserID int, pSchema, pTablename string, pOverwrite bool) error {
	if m.provider == nil {
		return fmt.Errorf("security provider not set")
	}

	secKey := fmt.Sprintf("%s.%s@%d", pSchema, pTablename, pUserID)

	if !pOverwrite {
		m.ColumnSecurityMutex.RLock()
		exp, ok := m.colSecExpiry[secKey]
		fresh := ok && m.ColumnSecurity[secKey] != nil && time.Now().Before(exp)
		m.ColumnSecurityMutex.RUnlock()
		if fresh {
			return nil
		}
	}

	// Query the provider without holding any lock.
	loadCtx, cancel := context.WithTimeout(ctx, securityLoadTimeout)
	defer cancel()
	colSecList, err := m.provider.GetColumnSecurity(loadCtx, pUserID, pSchema, pTablename)
	if err != nil {
		return fmt.Errorf("GetColumnSecurity failed: %v", err)
	}
	if colSecList == nil {
		colSecList = make([]ColumnSecurity, 0)
	}

	now := time.Now()
	m.ColumnSecurityMutex.Lock()
	defer m.ColumnSecurityMutex.Unlock()
	if m.ColumnSecurity == nil {
		m.ColumnSecurity = make(map[string][]ColumnSecurity)
	}
	if m.colSecExpiry == nil {
		m.colSecExpiry = make(map[string]time.Time)
	}
	m.ColumnSecurity[secKey] = colSecList
	m.colSecExpiry[secKey] = now.Add(securityCacheTTL)
	if now.Sub(m.lastColPrune) > securityCacheTTL {
		m.lastColPrune = now
		for k, exp := range m.colSecExpiry {
			if now.Sub(exp) > securityPruneGrace {
				delete(m.colSecExpiry, k)
				delete(m.ColumnSecurity, k)
			}
		}
	}
	return nil
}

func (m *SecurityList) ClearSecurity(pUserID int, pSchema, pTablename string) error {
	var filtered []ColumnSecurity
	m.ColumnSecurityMutex.Lock()
	defer m.ColumnSecurityMutex.Unlock()

	secKey := fmt.Sprintf("%s.%s@%d", pSchema, pTablename, pUserID)
	list, ok := m.ColumnSecurity[secKey]
	if !ok {
		return nil
	}

	for i := range list {
		cs := &list[i]
		if cs.Schema != pSchema && cs.Tablename != pTablename && cs.UserID != pUserID {
			filtered = append(filtered, *cs)
		}
	}

	m.ColumnSecurity[secKey] = filtered
	return nil
}

func (m *SecurityList) LoadRowSecurity(ctx context.Context, pUserRef any, pSchema, pTablename string, pOverwrite bool) (RowSecurity, error) {
	if m.provider == nil {
		return RowSecurity{}, fmt.Errorf("security provider not set")
	}

	secKey := fmt.Sprintf("%s.%s@%v", pSchema, pTablename, pUserRef)

	if !pOverwrite {
		m.RowSecurityMutex.RLock()
		exp, ok := m.rowSecExpiry[secKey]
		cached, present := m.RowSecurity[secKey]
		m.RowSecurityMutex.RUnlock()
		if ok && present && time.Now().Before(exp) {
			return cached, nil
		}
	}

	// Query the provider without holding any lock.
	loadCtx, cancel := context.WithTimeout(ctx, securityLoadTimeout)
	defer cancel()
	record, err := m.provider.GetRowSecurity(loadCtx, pUserRef, pSchema, pTablename)
	if err != nil {
		return RowSecurity{}, fmt.Errorf("GetRowSecurity failed: %v", err)
	}

	now := time.Now()
	m.RowSecurityMutex.Lock()
	defer m.RowSecurityMutex.Unlock()
	if m.RowSecurity == nil {
		m.RowSecurity = make(map[string]RowSecurity)
	}
	if m.rowSecExpiry == nil {
		m.rowSecExpiry = make(map[string]time.Time)
	}
	m.RowSecurity[secKey] = record
	m.rowSecExpiry[secKey] = now.Add(securityCacheTTL)
	if now.Sub(m.lastRowPrune) > securityCacheTTL {
		m.lastRowPrune = now
		for k, exp := range m.rowSecExpiry {
			if now.Sub(exp) > securityPruneGrace {
				delete(m.rowSecExpiry, k)
				delete(m.RowSecurity, k)
			}
		}
	}
	return record, nil
}

func (m *SecurityList) GetRowSecurityTemplate(pUserRef any, pSchema, pTablename string) (RowSecurity, error) {
	defer logger.CatchPanic("GetRowSecurityTemplate")()

	m.RowSecurityMutex.RLock()
	defer m.RowSecurityMutex.RUnlock()

	if m.RowSecurity == nil {
		return RowSecurity{}, fmt.Errorf("security not initialized")
	}

	rowSec, ok := m.RowSecurity[fmt.Sprintf("%s.%s@%v", pSchema, pTablename, pUserRef)]
	if !ok {
		return RowSecurity{}, fmt.Errorf("no row security data")
	}

	return rowSec, nil
}
