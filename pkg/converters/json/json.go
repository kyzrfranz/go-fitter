package json

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/muktihari/fit/decoder"
	"github.com/muktihari/fit/kit/datetime"
	"github.com/muktihari/fit/kit/scaleoffset"
	"github.com/muktihari/fit/kit/semicircles"
	"github.com/muktihari/fit/profile"
	"github.com/muktihari/fit/profile/basetype"
	"github.com/muktihari/fit/profile/mesgdef"
	"github.com/muktihari/fit/profile/untyped/mesgnum"
	"github.com/muktihari/fit/proto"
)

var (
	_ decoder.MesgDefListener = &Converter{}
	_ decoder.MesgListener    = &Converter{}
)

// Converter is an implementation for listeners that receive message events and convert them into a structured JSON object.
type Converter struct {
	err error // Error occurred while receiving messages.

	options *options

	fieldDescriptions []*mesgdef.FieldDescription

	// Slices to hold processed messages
	sessionMessages []map[string]any
	lapMessages     []map[string]any
	recordMessages  []map[string]any
	sportMessages   []map[string]any

	mesgc chan any      // This buffered event channel can accept either proto.Message or proto.MessageDefinition maintaining the order of arrival.
	done  chan struct{} // Tells that all messages have been completely processed.

	result string
}

type options struct {
	channelBufferSize         int
	useRawValue               bool // Use raw value instead of scaled value
	printOnlyValidValue       bool // Print only valid value
	printGPSPositionInDegrees bool // Print latitude and longitude in degrees instead of semicircles.
	prettyPrint               bool // Pretty-print the final JSON output
	noRecords                 bool // Add --no-records flag
}

// NewFITToJSONConv creates a new FIT to JSON converter.
func NewFITToJSONConv(opts ...Option) *Converter {
	options := defaultOptions()
	for i := range opts {
		opts[i](options)
	}

	c := &Converter{
		options:         options,
		sessionMessages: make([]map[string]any, 0),
		lapMessages:     make([]map[string]any, 0),
		recordMessages:  make([]map[string]any, 0),
		sportMessages:   make([]map[string]any, 0),
		mesgc:           make(chan any, options.channelBufferSize),
		done:            make(chan struct{}),
	}

	go c.handleEvent() // spawn only once.

	return c
}

// Err returns any error that occur during processing events.
func (c *Converter) Err() error { return c.err }

// OnMesgDef receive message definition from broadcaster
func (c *Converter) OnMesgDef(mesgDef proto.MessageDefinition) { c.mesgc <- mesgDef }

// OnMesg receive message from broadcaster
func (c *Converter) OnMesg(mesg proto.Message) { c.mesgc <- mesg }

// handleEvent processes events from a buffered channel.
func (c *Converter) handleEvent() {
	for event := range c.mesgc {
		switch mesg := event.(type) {
		case proto.Message:
			c.processMessage(mesg)
		case proto.MessageDefinition:
			// We don't need to do anything with Defs for JSON
		}
	}
	close(c.done)
}

// processMessage routes the message to the correct slice
func (c *Converter) processMessage(mesg proto.Message) {
	if c.err != nil {
		return
	}

	if mesg.Num == mesgnum.FieldDescription {
		c.fieldDescriptions = append(c.fieldDescriptions, mesgdef.NewFieldDescription(&mesg))
		return
	}

	mesgMap := c.buildMessageMap(mesg)
	if mesgMap == nil {
		return
	}

	switch mesg.Num {
	case mesgnum.Session:
		c.sessionMessages = append(c.sessionMessages, mesgMap)
	case mesgnum.Lap:
		c.lapMessages = append(c.lapMessages, mesgMap)
	case mesgnum.Record:
		c.recordMessages = append(c.recordMessages, mesgMap)
	case mesgnum.Sport:
		c.sportMessages = append(c.sportMessages, mesgMap)
	}
}

// buildMessageMap is the core logic. It converts a proto.Message into a map[string]any
func (c *Converter) buildMessageMap(mesg proto.Message) map[string]any {
	mesgMap := make(map[string]any)

	// Process standard fields
	for i := range mesg.Fields {
		field := &mesg.Fields[i]
		if field.IsExpandedField { // Skip component-expanded fields
			continue
		}

		if c.options.printOnlyValidValue && !field.Value.Valid(field.BaseType) {
			continue
		}

		name, units := field.Name, field.Units
		scale, offset := field.Scale, field.Offset
		value := field.Value
		profileType := field.Type
		baseType := field.BaseType

		// Check for subfield substitution
		if subField := field.SubFieldSubtitution(&mesg); subField != nil { // Uses 'mesg'
			name = subField.Name
			units = subField.Units
			scale = subField.Scale
			offset = subField.Offset
			profileType = subField.Type
			baseType = subField.Type.BaseType()
			value = castValue(value, baseType) // Cast value to subfield's base type
		}

		var finalValue any

		// --- Handle Slices Explicitly First ---
		switch value.Type() {
		case proto.TypeSliceInt8:
			finalValue = value.SliceInt8()

		case proto.TypeSliceUint8:
			// Must convert []uint8 to []int, otherwise json.Marshal will Base64 encode it.
			s := value.SliceUint8()
			i := make([]int, len(s))
			for k, v := range s {
				i[k] = int(v)
			}
			finalValue = i

		case proto.TypeSliceInt16:
			finalValue = value.SliceInt16()
		case proto.TypeSliceUint16:
			finalValue = value.SliceUint16()
		case proto.TypeSliceInt32:
			finalValue = value.SliceInt32()
		case proto.TypeSliceUint32:
			finalValue = value.SliceUint32()
		case proto.TypeSliceInt64:
			finalValue = value.SliceInt64()
		case proto.TypeSliceUint64:
			finalValue = value.SliceUint64()
		case proto.TypeSliceFloat32:
			s := value.SliceFloat32()
			clean := make([]float32, 0, len(s))
			for _, v := range s {
				f64 := float64(v)
				if !math.IsNaN(f64) && !math.IsInf(f64, 0) {
					clean = append(clean, v)
				}
			}
			finalValue = clean
		case proto.TypeSliceFloat64:
			s := value.SliceFloat64()
			clean := make([]float64, 0, len(s))
			for _, v := range s {
				if !math.IsNaN(v) && !math.IsInf(v, 0) {
					clean = append(clean, v)
				}
			}
			finalValue = clean
		case proto.TypeSliceString:
			finalValue = value.SliceString()

		default:
			// --- Logic for single (non-slice) values ---
			if c.options.useRawValue {
				finalValue = value.Any()
			} else {
				finalValue = scaleoffset.ApplyValue(value, scale, offset).Any()
			}

			// --- Special Value Conversions for JSON ---
			if c.options.printGPSPositionInDegrees && units == "semicircles" {
				finalValue = semicircles.ToDegrees(value.Int32())
			}
			switch profileType {
			case profile.DateTime, profile.LocalDateTime:
				finalValue = datetime.ToTime(value.Uint32()).Format(time.RFC3339)
			}
			if value.Type() == proto.TypeString {
				finalValue = value.String()
			}
			switch v := finalValue.(type) {
			case float64:
				if math.IsNaN(v) || math.IsInf(v, 0) {
					continue // Skip this field
				}
			case float32:
				f64 := float64(v)
				if math.IsNaN(f64) || math.IsInf(f64, 0) {
					continue // Skip this field
				}
			}
		}

		mesgMap[name] = finalValue
	}

	// Process developer fields
	for i := range mesg.DeveloperFields {
		devField := &mesg.DeveloperFields[i]
		fieldDesc := c.getFieldDescription(devField.DeveloperDataIndex, devField.Num)
		if fieldDesc == nil {
			continue
		}

		name := strings.Join(fieldDesc.FieldName, "|")
		finalDevValue := devField.Value.Any()

		switch v := finalDevValue.(type) {
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
		case float32:
			f64 := float64(v)
			if math.IsNaN(f64) || math.IsInf(f64, 0) {
				continue
			}
		}

		mesgMap[name] = finalDevValue
	}

	if len(mesgMap) == 0 {
		return nil
	}
	return mesgMap
}

// Wait closes the buffered channel and waits until all event handling is completed
// and then marshals and writes the final JSON.
func (c *Converter) Wait() {
	close(c.mesgc)
	<-c.done
	c.result = c.marshal()
}

func (c *Converter) Result() string {
	return c.result
}

// marshalAndWrite collates all processed data and writes it as a single JSON object.
func (c *Converter) marshal() string {
	if c.err != nil { // Check for earlier processing errors
		return ""
	}

	c.enrichLaps()

	// Collate all data into the final coach-friendly structure
	finalData := make(map[string]any)

	// We only expect one session and one sport message
	if len(c.sessionMessages) > 0 {
		finalData["sessionSummary"] = c.sessionMessages[0]
	}
	if len(c.sportMessages) > 0 {
		finalData["sport"] = c.sportMessages[0]
	}

	// This 'c.lapMessages' slice now contains the ENRICHED laps
	finalData["laps"] = c.lapMessages

	// This check is from our previous step.
	// Use --no-records to get a small, clean file.
	if !c.options.noRecords {
		finalData["records"] = c.recordMessages
	}

	// Marshal to JSON
	var jsonData []byte
	var err error
	if c.options.prettyPrint {
		jsonData, err = json.MarshalIndent(finalData, "", "  ")
	} else {
		jsonData, err = json.Marshal(finalData)
	}
	if err != nil {
		c.err = fmt.Errorf("marshal json: %w", err)
		return ""
	}

	return string(jsonData)
}

// getFieldDescription finds the matching FieldDescription for a developer field.
func (c *Converter) getFieldDescription(developerDataIndex, fieldDefinitionNumber uint8) *mesgdef.FieldDescription {
	for _, fieldDesc := range c.fieldDescriptions {
		if fieldDesc.DeveloperDataIndex == developerDataIndex &&
			fieldDesc.FieldDefinitionNumber == fieldDefinitionNumber {
			return fieldDesc
		}
	}
	return nil
}

// castValue cast any integer value into targeted baseType.
func castValue(val proto.Value, baseType basetype.BaseType) proto.Value {
	var value uint64
	switch val.Type() {
	case proto.TypeInt8:
		value = uint64(val.Int8())
	case proto.TypeUint8:
		value = uint64(val.Uint8())
	case proto.TypeInt16:
		value = uint64(val.Int16())
	case proto.TypeUint16:
		value = uint64(val.Uint16())
	case proto.TypeInt32:
		value = uint64(val.Int32())
	case proto.TypeUint32:
		value = uint64(val.Uint32())
	case proto.TypeInt64:
		value = uint64(val.Int64())
	case proto.TypeUint64:
		value = uint64(val.Uint64())
	default:
		return val // Not an integer type, can't cast
	}

	switch baseType {
	case basetype.Sint8:
		return proto.Int8(int8(value))
	case basetype.Enum, basetype.Uint8, basetype.Uint8z:
		return proto.Uint8(uint8(value))
	case basetype.Sint16:
		return proto.Int16(int16(value))
	case basetype.Uint16, basetype.Uint16z:
		return proto.Uint16(uint16(value))
	case basetype.Sint32:
		return proto.Int32(int32(value))
	case basetype.Uint32, basetype.Uint32z:
		return proto.Uint32(uint32(value))
	case basetype.Sint64:
		return proto.Int64(int64(value))
	case basetype.Uint64, basetype.Uint64z:
		return proto.Uint64(uint64(value))
	}

	return val
}

// --- NEW HELPER FUNCTION ---
// getFloat safely gets a float64 from a map[string]any,
// converting from any numeric type.
func getFloat(m map[string]any, key string) (float64, bool) {
	val, ok := m[key]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// --- FINAL-FINAL enrichLaps FUNCTION ---
func (c *Converter) enrichLaps() {
	if len(c.recordMessages) == 0 || len(c.lapMessages) == 0 {
		return
	}

	for i := range c.lapMessages {
		lap := c.lapMessages[i]

		lapStartTimeStr, ok := lap["start_time"].(string)
		if !ok {
			continue
		}
		lapDuration, ok := getFloat(lap, "total_timer_time")
		if !ok {
			continue
		}
		lapStartTime, err := time.Parse(time.RFC3339, lapStartTimeStr)
		if err != nil {
			continue
		}
		nanoseconds := int64(lapDuration * float64(time.Second))
		lapEndTime := lapStartTime.Add(time.Duration(nanoseconds))

		// --- 2. Define aggregates for ALL "Data Gold" ---
		type accumulator struct {
			sum   float64
			count int
		}

		// We will map the raw 'record' key to the new 'lap' key
		// format: recordKey: {accumulator, newLapKey}
		aggMap := map[string]*struct {
			acc    accumulator
			newKey string
		}{
			// Stryd Developer Fields
			"Power":                {newKey: "avg_stryd_power"},
			"Air Power":            {newKey: "avg_air_power"},
			"Form Power":           {newKey: "avg_form_power"},
			"Ground Time":          {newKey: "avg_stryd_ground_time"},
			"Impact Loading Rate":  {newKey: "avg_impact_loading_rate"},
			"Leg Spring Stiffness": {newKey: "avg_leg_spring_stiffness"},
			"Vertical Oscillation": {newKey: "avg_stryd_vo"},

			// Garmin Running Dynamics (Standard Fields)
			"stance_time":          {newKey: "avg_garmin_stance_time"},
			"stance_time_balance":  {newKey: "avg_garmin_stance_time_balance"},
			"vertical_oscillation": {newKey: "avg_garmin_vo"},
			"vertical_ratio":       {newKey: "avg_garmin_vertical_ratio"},
			"step_length":          {newKey: "avg_garmin_step_length"},

			// Cycling Dynamics (add more as needed)
			// "left_torque_effectiveness":  {newKey: "avg_left_torque_effectiveness"},
			// "right_torque_effectiveness": {newKey: "avg_right_torque_effectiveness"},
			// "left_pco":                   {newKey: "avg_left_pco"},
			// "right_pco":                  {newKey: "avg_right_pco"},
		}

		// --- 3. Iterate all records ---
		for _, record := range c.recordMessages {
			recordTimeStr, ok := record["timestamp"].(string)
			if !ok {
				continue
			}
			recordTime, err := time.Parse(time.RFC3339, recordTimeStr)
			if err != nil {
				continue
			}

			if (recordTime.After(lapStartTime) || recordTime.Equal(lapStartTime)) && recordTime.Before(lapEndTime) {
				// --- 4. Aggregate data! ---
				for key, data := range aggMap {
					if value, ok := getFloat(record, key); ok {
						data.acc.sum += value
						data.acc.count++
					}
				}
			}
		} // end for recordMessages

		// --- 5. Add all new averages to the lap map ---
		for _, data := range aggMap {
			if data.acc.count > 0 {
				lap[data.newKey] = data.acc.sum / float64(data.acc.count)
			}
		}
	} // end for lapMessages
}
