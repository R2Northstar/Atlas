package nstypes

import "strconv"

type Playlist string

const (
	// vanilla
	PrivateMatch           Playlist = "private_match"
	Attrition              Playlist = "aitdm"
	BountyHunt             Playlist = "at"
	Coliseum               Playlist = "coliseum"
	AmpedHardpoint         Playlist = "cp"
	CaptureTheFlag         Playlist = "ctf"
	FrontierDefenseEasy    Playlist = "fd_easy"
	FrontierDefenseHard    Playlist = "fd_hard"
	FrontierDefenseInsane  Playlist = "fd_insane"
	FrontierDefenseMaster  Playlist = "fd_master"
	FrontierDefenseRegular Playlist = "fd_normal"
	LastTitanStanding      Playlist = "lts"
	MarkedForDeath         Playlist = "mfd"
	PilotsVsPilots         Playlist = "ps"
	Campaign               Playlist = "solo"
	Skirmish               Playlist = "tdm"
	TitanBrawl             Playlist = "ttdm"
	LiveFire               Playlist = "lf" // mp_gamemode speedball

	// vanilla featured
	AegisLastTitanStanding Playlist = "alts"
	AegisTitanBrawl        Playlist = "attdm"
	FreeForAll             Playlist = "ffa"
	FreeAgents             Playlist = "fra"
	TheGreatBamboozle      Playlist = "holopilot_lf"
	RocketArena            Playlist = "rocket_lf"
	TurboLastTitanStanding Playlist = "turbo_lts"
	TurboTitanBrawl        Playlist = "turbo_ttdm"

	// Northstar.Custom
	OneInTheChamber Playlist = "chamber"
	CompetitiveCTF  Playlist = "ctf_comp"
	Fastball        Playlist = "fastball"
	FrontierWar     Playlist = "fw"
	GunGame         Playlist = "gg"
	TheHidden       Playlist = "hidden"
	HideAndSeek     Playlist = "hs"
	Infection       Playlist = "inf"
	AmpedKillrace   Playlist = "kr"
	SticksAndStones Playlist = "sns"
	TitanFFA        Playlist = "tffa"
	TitanTag        Playlist = "tt"

	// Northstar.Coop
	SingleplayerCoop Playlist = "sp_coop"
)

// Playalists gets all known playlists.
func Playlists() []Playlist {
	return []Playlist{
		PrivateMatch,
		Attrition,
		BountyHunt,
		Coliseum,
		AmpedHardpoint,
		CaptureTheFlag,
		FrontierDefenseEasy,
		FrontierDefenseHard,
		FrontierDefenseInsane,
		FrontierDefenseMaster,
		FrontierDefenseRegular,
		LastTitanStanding,
		MarkedForDeath,
		PilotsVsPilots,
		Campaign,
		Skirmish,
		TitanBrawl,
		LiveFire,
		AegisLastTitanStanding,
		AegisTitanBrawl,
		FreeForAll,
		FreeAgents,
		TheGreatBamboozle,
		RocketArena,
		TurboLastTitanStanding,
		TurboTitanBrawl,
		OneInTheChamber,
		CompetitiveCTF,
		Fastball,
		FrontierWar,
		GunGame,
		TheHidden,
		HideAndSeek,
		Infection,
		AmpedKillrace,
		SticksAndStones,
		TitanFFA,
		TitanTag,
		SingleplayerCoop,
	}
}

// GoString gets the map in Go syntax.
func (p Playlist) GoString() string {
	return "Playlist(" + strconv.Quote(string(p)) + ")"
}

// SourceString gets the raw playlist name.
func (p Playlist) SourceString() string {
	return string(p)
}

// Known checks if the playlist is a known Northstar playlist.
func (p Playlist) Known() bool {
	_, ok := p.Title()
	return ok
}

// String returns the title or raw playlist name.
func (p Playlist) String() string {
	if t, ok := p.Title(); ok {
		return t
	}
	return p.SourceString()
}

// Title returns the title of known Northstar playlists.
func (p Playlist) Title() (string, bool) {
	switch p {
	case "private_match":
		return "Private Match", true
	case "aitdm":
		return "Attrition", true
	case "at":
		return "Bounty Hunt", true
	case "coliseum":
		return "Coliseum", true
	case "cp":
		return "Amped Hardpoint", true
	case "ctf":
		return "Capture the Flag", true
	case "fd_easy":
		return "Frontier Defense (Easy)", true
	case "fd_hard":
		return "Frontier Defense (Hard)", true
	case "fd_insane":
		return "Frontier Defense (Insane)", true
	case "fd_master":
		return "Frontier Defense (Master)", true
	case "fd_normal":
		return "Frontier Defense (Regular)", true
	case "lts":
		return "Last Titan Standing", true
	case "mfd":
		return "Marked For Death", true
	case "ps":
		return "Pilots vs. Pilots", true
	case "solo":
		return "Campaign", true
	case "tdm":
		return "Skirmish", true
	case "ttdm":
		return "Titan Brawl", true
	case "lf":
		return "Live Fire", true
	case "alts":
		return "Aegis Last Titan Standing", true
	case "attdm":
		return "Aegis Titan Brawl", true
	case "ffa":
		return "Free For All", true
	case "fra":
		return "Free Agents", true
	case "holopilot_lf":
		return "The Great Bamboozle", true
	case "rocket_lf":
		return "Rocket Arena", true
	case "turbo_lts":
		return "Turbo Last Titan Standing", true
	case "turbo_ttdm":
		return "Turbo Titan Brawl", true
	case "chamber":
		return "One in the Chamber", true
	case "ctf_comp":
		return "Competitive CTF", true
	case "fastball":
		return "Fastball", true
	case "fw":
		return "Frontier War", true
	case "gg":
		return "Gun Game", true
	case "hidden":
		return "The Hidden", true
	case "hs":
		return "Hide and Seek", true
	case "inf":
		return "Infection", true
	case "kr":
		return "Amped Killrace", true
	case "sns":
		return "Sticks and Stones", true
	case "tffa":
		return "Titan FFA", true
	case "tt":
		return "Titan Tag", true
	case "sp_coop":
		return "Singleplayer Coop", true
	}
	return "", false
}
