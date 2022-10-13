package nstypes

import "strconv"

type Map string

const (
	AngelCity              Map = "mp_angel_city"
	BlackWaterCanal        Map = "mp_black_water_canal"
	Box                    Map = "mp_box"
	MapColiseum            Map = "mp_coliseum"
	Pillars                Map = "mp_coliseum_column"
	Colony                 Map = "mp_colony02"
	Complex                Map = "mp_complex3"
	CrashSite              Map = "mp_crashsite3"
	Drydock                Map = "mp_drydock"
	Eden                   Map = "mp_eden"
	ForwardbaseKodai       Map = "mp_forwardbase_kodai"
	Glitch                 Map = "mp_glitch"
	Boomtown               Map = "mp_grave"
	Homestead              Map = "mp_homestead"
	Deck                   Map = "mp_lf_deck"
	Meadow                 Map = "mp_lf_meadow"
	Stacks                 Map = "mp_lf_stacks"
	Township               Map = "mp_lf_township"
	Traffic                Map = "mp_lf_traffic"
	UMA                    Map = "mp_lf_uma"
	Lobby                  Map = "mp_lobby"
	Relic                  Map = "mp_relic02"
	Rise                   Map = "mp_rise"
	Exoplanet              Map = "mp_thaw"
	WarGames               Map = "mp_wargames"
	ThePilotsGauntlet      Map = "sp_training"
	BT7274                 Map = "sp_crashsite"
	BloodAndRust           Map = "sp_sewers1"
	IntoTheAbyssPart1      Map = "sp_boomtown_start"
	IntoTheAbyssPart2A     Map = "sp_boomtown"
	IntoTheAbyssPart2B     Map = "sp_boomtown_end"
	EffectAndCausePart1or3 Map = "sp_hub_timeshift"
	EffectAndCausePart2    Map = "sp_timeshift_spoke02"
	TheBeaconPart1or3      Map = "sp_beacon"
	TheBeaconPart2         Map = "sp_beacon_spoke0"
	TrialByFire            Map = "sp_tday"
	TheArk                 Map = "sp_s2s"
	TheFoldWeapon          Map = "sp_skyway_v1"
)

// Maps gets all known maps.
func Maps() []Map {
	return []Map{
		AngelCity,
		BlackWaterCanal,
		Box,
		MapColiseum,
		Pillars,
		Colony,
		Complex,
		CrashSite,
		Drydock,
		Eden,
		ForwardbaseKodai,
		Glitch,
		Boomtown,
		Homestead,
		Deck,
		Meadow,
		Stacks,
		Township,
		Traffic,
		UMA,
		Lobby,
		Relic,
		Rise,
		Exoplanet,
		WarGames,
		ThePilotsGauntlet,
		BT7274,
		BloodAndRust,
		IntoTheAbyssPart1,
		IntoTheAbyssPart2A,
		IntoTheAbyssPart2B,
		EffectAndCausePart1or3,
		EffectAndCausePart2,
		TheBeaconPart1or3,
		TheBeaconPart2,
		TrialByFire,
		TheArk,
		TheFoldWeapon,
	}
}

// GoString gets the map in Go syntax.
func (m Map) GoString() string {
	return "Map(" + strconv.Quote(string(m)) + ")"
}

// SourceString gets the raw map name.
func (m Map) SourceString() string {
	return string(m)
}

// Known checks if the map is a known Northstar map.
func (m Map) Known() bool {
	_, ok := m.Title()
	return ok
}

// String returns the title or raw map name.
func (m Map) String() string {
	if t, ok := m.Title(); ok {
		return t
	}
	return m.SourceString()
}

// Title returns the title of known Northstar maps.
func (m Map) Title() (string, bool) {
	switch m {
	case "mp_angel_city":
		return "Angel City", true
	case "mp_black_water_canal":
		return "Black Water Canal", true
	case "mp_box":
		return "Box", true
	case "mp_coliseum":
		return "Coliseum", true
	case "mp_coliseum_column":
		return "Pillars", true
	case "mp_colony02":
		return "Colony", true
	case "mp_complex3":
		return "Complex", true
	case "mp_crashsite3":
		return "Crash Site", true
	case "mp_drydock":
		return "Drydock", true
	case "mp_eden":
		return "Eden", true
	case "mp_forwardbase_kodai":
		return "Forwardbase Kodai", true
	case "mp_glitch":
		return "Glitch", true
	case "mp_grave":
		return "Boomtown", true
	case "mp_homestead":
		return "Homestead", true
	case "mp_lf_deck":
		return "Deck", true
	case "mp_lf_meadow":
		return "Meadow", true
	case "mp_lf_stacks":
		return "Stacks", true
	case "mp_lf_township":
		return "Township", true
	case "mp_lf_traffic":
		return "Traffic", true
	case "mp_lf_uma":
		return "UMA", true
	case "mp_lobby":
		return "Lobby", true
	case "mp_relic02":
		return "Relic", true
	case "mp_rise":
		return "Rise", true
	case "mp_thaw":
		return "Exoplanet", true
	case "mp_wargames":
		return "War Games", true
	case "sp_training":
		return "The Pilot's Gauntlet", true
	case "sp_crashsite":
		return "BT-7274", true
	case "sp_sewers1":
		return "Blood and Rust", true
	case "sp_boomtown_start":
		return "Into the Abyss - Part 1", true
	case "sp_boomtown":
		return "Into the Abyss - Part 2", true
	case "sp_boomtown_end":
		return "Into the Abyss - Part 2", true
	case "sp_hub_timeshift":
		return "Effect and Cause - Part 1 or 3", true
	case "sp_timeshift_spoke02":
		return "Effect and Cause - Part 2", true
	case "sp_beacon":
		return "The Beacon - Part 1 or 3", true
	case "sp_beacon_spoke0":
		return "The Beacon - Part 2", true
	case "sp_tday":
		return "Trial by Fire", true
	case "sp_s2s":
		return "The Ark", true
	case "sp_skyway_v1":
		return "The Fold Weapon", true
	}
	return "", false
}
