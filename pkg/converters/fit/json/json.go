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
	"github.com/muktihari/fit/profile/typedef"
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
	noRecords                 bool // Skip Records as they blow up size
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
		if subField := field.SubFieldSubstitution(&mesg); subField != nil { // Uses 'mesg'
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
			case profile.Sport:
				finalValue = typedef.Sport(value.Uint8()).String()
			case profile.SubSport:
				finalValue = typedef.SubSport(value.Uint8()).String()
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

// leg is one session together with the records and laps that fall inside its
// time window. A single-sport file has exactly one leg; a multisport (triathlon)
// file has one per session (swim, T1, bike, T2, run).
type leg struct {
	session map[string]any
	records []map[string]any
	laps    []map[string]any
}

// marshal collates the decoded messages into one JSON object per non-transition
// session ("leg") and emits them as a JSON array. A normal single-sport file
// yields a one-element array. Transition sessions (sport == "transition") are
// dropped along with the records/laps that fall in their window, so they can't
// corrupt an adjacent leg.
func (c *Converter) marshal() string {
	if c.err != nil { // Check for earlier processing errors
		return ""
	}

	legs := c.partitionLegs()

	out := make([]map[string]any, 0, len(legs))
	for i := range legs {
		l := legs[i]
		sport := getString(l.session, "sport")
		if sport == "transition" {
			continue // transitions are not real legs; their records/laps are dropped
		}

		// Enrich this leg's laps using only this leg's records.
		c.enrichLaps(l.session, l.laps, l.records, sport)

		finalData := map[string]any{
			"sessionSummary": l.session,
			"laps":           l.laps,
		}
		if sm := c.sportMessageFor(sport); sm != nil {
			finalData["sportMesg"] = sm
		}
		// Use --no-records to get a small, clean file.
		if !c.options.noRecords {
			finalData["records"] = l.records
		}
		out = append(out, finalData)
	}

	// Marshal to JSON (always an array, even for a single leg).
	var jsonData []byte
	var err error
	if c.options.prettyPrint {
		jsonData, err = json.MarshalIndent(out, "", "  ")
	} else {
		jsonData, err = json.Marshal(out)
	}
	if err != nil {
		c.err = fmt.Errorf("marshal json: %w", err)
		return ""
	}

	return string(jsonData)
}

// partitionLegs splits the global record and lap streams across the sessions by
// time. Each record/lap is assigned to the session with the latest start_time
// that is still <= its own timestamp. Sessions arrive in file (chronological)
// order, so this partitions the timeline with no overlap and nothing
// double-counted: a record in a transition window lands on the transition
// session and is later dropped, never leaking into the swim/bike/run legs.
func (c *Converter) partitionLegs() []leg {
	legs := make([]leg, len(c.sessionMessages))
	starts := make([]time.Time, len(c.sessionMessages))
	for i, s := range c.sessionMessages {
		legs[i] = leg{session: s}
		if t, ok := parseTime(s, "start_time"); ok {
			starts[i] = t
		}
	}

	// assign returns the index of the owning session for timestamp t, or -1.
	assign := func(t time.Time) int {
		idx := -1
		for i := range starts {
			if starts[i].IsZero() {
				continue
			}
			if !t.Before(starts[i]) { // t >= start_i; later sessions start later, so last match wins
				idx = i
			}
		}
		if idx == -1 && len(legs) > 0 {
			idx = 0 // a stray record before the first session folds into the first leg
		}
		return idx
	}

	for _, rec := range c.recordMessages {
		t, ok := parseTime(rec, "timestamp")
		if !ok {
			continue
		}
		if i := assign(t); i >= 0 {
			legs[i].records = append(legs[i].records, rec)
		}
	}
	for _, lap := range c.lapMessages {
		t, ok := parseTime(lap, "start_time")
		if !ok {
			continue
		}
		if i := assign(t); i >= 0 {
			legs[i].laps = append(legs[i].laps, lap)
		}
	}
	return legs
}

// sportMessageFor returns the sport message whose "sport" matches the leg's
// sport, or nil. Multisport files carry one sport message per leg.
func (c *Converter) sportMessageFor(sport string) map[string]any {
	for _, sm := range c.sportMessages {
		if getString(sm, "sport") == sport {
			return sm
		}
	}
	return nil
}

// parseTime reads an RFC3339 timestamp from m[key].
func parseTime(m map[string]any, key string) (time.Time, bool) {
	s, ok := m[key].(string)
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func getString(m map[string]any, key string) string {
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
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

// enrichLaps decorates one leg's laps with per-lap averages derived from that
// leg's records, then rolls leg-level totals onto the leg's session. It operates
// only on the records and laps belonging to this leg so a multisport file's legs
// never bleed into each other.
func (c *Converter) enrichLaps(session map[string]any, laps, records []map[string]any, sport string) {
	if len(records) == 0 || len(laps) == 0 {
		return
	}

	var sessionTotalMovingTime float64
	var sessionTotalStrokes float64
	var sessionAvgRunningPower float64

	for i := range laps {
		lap := laps[i]

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
			"left_torque_effectiveness":  {newKey: "avg_left_torque_effectiveness"},
			"right_torque_effectiveness": {newKey: "avg_right_torque_effectiveness"},
			"left_pco":                   {newKey: "avg_left_pco"},
			"right_pco":                  {newKey: "avg_right_pco"},

			// --- SWIM FIELDS ---
			// Note: Cadence is used for both swim and bike/run in records
			"cadence": {newKey: "avg_cadence"},
		}

		// --- 3. Iterate this leg's records ---
		var lapMovingTime float64 // We need to calculate this for swims
		for _, record := range records {
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
						if sport == "swimming" && key == "cadence" && value == 0 {
							continue // Don't count 0 spm
						}
						data.acc.sum += value
						data.acc.count++
					}
				}
				if speed, ok := getFloat(record, "speed"); ok && speed > 0 {
					lapMovingTime += 1 // Assumes 1-second records
				}
			}
		} // end for recordMessages

		// --- 5. Add all new averages to the lap map ---
		for _, data := range aggMap {
			if data.acc.count > 0 {
				lap[data.newKey] = data.acc.sum / float64(data.acc.count)
			}
		}

		if sport == "swimming" {
			// Overwrite moving time with our calculated value
			lap["total_moving_time"] = lapMovingTime
			sessionTotalMovingTime += lapMovingTime // Add to session total

			if avgCadence, ok := getFloat(lap, "avg_cadence"); ok && lapMovingTime > 0 {
				// total_strokes = strokes_per_minute * minutes_moving
				lapTotalStrokes := avgCadence * (lapMovingTime / 60.0)
				lap["total_strokes"] = int(lapTotalStrokes)
				sessionTotalStrokes += lapTotalStrokes // Add to session total

				// Now calculate stroke distance
				if lapDist, ok := getFloat(lap, "total_distance"); ok && lapTotalStrokes > 0 {
					lap["avg_stroke_distance"] = lapDist / lapTotalStrokes
				}
			}
		}

		if sport == "running" {
			// for running accumulate avg running power
			if avgRuningPower, ok := getFloat(lap, "avg_stryd_power"); ok && avgRuningPower > 0 {
				sessionAvgRunningPower += avgRuningPower
			}
		}

	} // end for laps

	if sport == "swimming" {
		session["total_moving_time"] = sessionTotalMovingTime
		session["total_strokes"] = int(sessionTotalStrokes)

		if totalDist, ok := getFloat(session, "total_distance"); ok && sessionTotalStrokes > 0 {
			session["avg_stroke_distance"] = totalDist / sessionTotalStrokes
		}
	}
	if sport == "running" && len(laps) > 0 {
		session["avg_running_power"] = sessionAvgRunningPower / float64(len(laps))
	}
}
