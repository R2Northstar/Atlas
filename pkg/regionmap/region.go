// Package regionmap maps IP address location info to region names.
//
// The region names are primarily determined based on the country, but also use
// state/province/region names for larger countries. The mapping is based on
// a combination of data from:
//
//   - RFC 1918 private IPv4 addresses (https://www.rfc-editor.org/rfc/rfc1918).
//   - RFC 4193 private IPv6 addresses (https://www.rfc-editor.org/rfc/rfc4193).
//   - UN M.49 (https://unstats.un.org/unsd/methodology/m49/overview/).
//   - US Census (https://www2.census.gov/geo/pdfs/maps-data/maps/reference/us_regdiv.pdf).
//   - Canada 3-region model (https://en.wikipedia.org/wiki/List_of_regions_of_Canada).
//   - IP2Location region names (https://www.ip2location.com/free/iso3166-2).
//
// The current mapping was created on November 20, 2022.
package regionmap

import (
	"fmt"
	"net/netip"

	"github.com/pg9182/ip2x"
)

// GetRegion gets the region name for the provided IP address and IP2Location
// record. The IP2Location record should have at least CountryShort and Region
// fields. If the location is unrecognized, a best-effort region and an error is
// returned.
func GetRegion(ip netip.Addr, r ip2x.Record) (string, error) {
	// RFC 1918/4193 -> "Local"
	if ip.IsPrivate() {
		return "Local", nil
	}

	country, ok := r.GetString(ip2x.CountryCode)
	if !ok {
		return "", fmt.Errorf("missing country field in ip2location data")
	}

	region, ok := r.GetString(ip2x.Region)
	if !ok {
		return "", fmt.Errorf("missing region field in ip2location data")
	}

	// for Canada, use the 3-region model
	if country == "CA" {
		// province names: https://www.ip2location.com/free/iso3166-2 @ 2022-11-20
		// 3-region model: https://en.wikipedia.org/wiki/List_of_regions_of_Canada @ 2022-11-20
		switch region {
		case "British Columbia", "Alberta", "Saskatchewan", "Manitoba":
			return "CA West", nil

		case "Ontario", "Quebec", "New Brunswick", "Prince Edward Island",
			"Nova Scotia", "Newfoundland and Labrador":
			return "CA East", nil

		case "Yukon", "Northwest Territories", "Nunavut":
			return "CA North", nil

		case "":
			return "CA", nil

		default:
			return "CA", fmt.Errorf("unhandled Canada province %q", region)
		}
	}

	// for the United States, use the census regions
	if country == "US" {
		// state names: https://www.ip2location.com/free/iso3166-2 @ 2022-11-20
		// census region: https://www2.census.gov/geo/pdfs/maps-data/maps/reference/us_regdiv.pdf @ 2022-11-20
		switch region {
		case "Connecticut", "Maine", "Massachusetts", "New Hampshire",
			"Rhode Island", "Vermont", "New Jersey", "New York", "Pennsylvania":
			return "US East", nil

		case "Indiana", "Illinois", "Michigan", "Ohio", "Wisconsin", "Iowa",
			"Kansas", "Minnesota", "Missouri", "Nebraska", "North Dakota",
			"South Dakota":
			return "US Central", nil

		case "Delaware", "District of Columbia", "Florida", "Georgia",
			"Maryland", "North Carolina", "South Carolina", "Virginia",
			"West Virginia", "Alabama", "Kentucky", "Mississippi",
			"Tennessee", "Arkansas", "Louisiana", "Oklahoma", "Texas":
			return "US South", nil

		case "Arizona", "Colorado", "Idaho", "New Mexico", "Montana",
			"Utah", "Nevada", "Wyoming", "Alaska", "California",
			"Hawaii", "Oregon", "Washington":
			return "US West", nil

		case "":
			return "US", nil

		default:
			return "US", fmt.Errorf("unhandled US state %q", region)
		}
	}

	// for China, use "CN"
	if country == "CN" {
		return "CN", nil
	}

	// for Russia, use "RU"
	if country == "RU" {
		return "RU", nil
	}

	// for Antartica, use Antartica (this won't really get hit in practice though)
	if country == "AQ" {
		return "Antartica", nil
	}

	// for Taiwan, use "Asia East" (it isn't in the UN M.49 mapping)
	if country == "TW" {
		return "Asia East", nil
	}

	// the rest are based on the M.49 mapping
	m49region, m49subRegion, _, ok := m49(country)
	if !ok {
		return "", fmt.Errorf("unhandled UN M.49 mapping for ISO 3166-2 code %q", country)
	}

	// for other parts of America, use "Americas"
	if m49region == "Americas" {
		return "Americas", nil
	}

	// group Oceania (Australia/NZ/Polynesia/Micronesia) into "AUS" since people
	// may not recognize "Oceania"
	if m49region == "Oceania" {
		return "AUS", nil
	}

	// group Africa together to keep things neat (and there aren't really
	// servers there anyways)
	if m49region == "Africa" {
		return "Africa", nil
	}

	// for Europe, use the M.49 sub-region name, but mangle it to sort better
	if m49region == "Europe" {
		switch m49subRegion {
		case "Eastern Europe":
			return "EU East", nil
		case "Northern Europe":
			return "EU North", nil
		case "Southern Europe":
			return "EU South", nil
		case "Western Europe":
			return "EU West", nil
		default:
			return "EU", fmt.Errorf("unhandled M.49 %s sub-region %q", m49region, m49subRegion)
		}
	}

	// for Asia, do the same thing
	if m49region == "Asia" {
		switch m49subRegion {
		case "Central Asia":
			return "Asia Central", nil
		case "Eastern Asia", "South-eastern Asia":
			return "Asia East", nil
		case "Southern Asia":
			return "Asia South", nil
		case "Western Asia":
			return "Asia West", nil
		default:
			return "Asia", fmt.Errorf("unhandled M.49 %s sub-region %q", m49region, m49subRegion)
		}
	}

	// for everything else, just use the M.49 region name
	return m49region, nil
}

func m49(iso3166_2 string) (region, subRegion, intermediateRegion string, ok bool) {
	// https://unstats.un.org/unsd/methodology/m49/overview/ @ 2022-11-20
	switch iso3166_2 {
	// cat m49.tsv | cut -d $'\t' -f4,6,8,9,11 | tr '\t' '\a' | while IFS=$'\a' read -r a b c d e; do echo "case \"$e\": // $d"; echo "return \"$a\", \"$b\", \"$c\", true"; done
	case "DZ": // Algeria
		return "Africa", "Northern Africa", "", true
	case "EG": // Egypt
		return "Africa", "Northern Africa", "", true
	case "LY": // Libya
		return "Africa", "Northern Africa", "", true
	case "MA": // Morocco
		return "Africa", "Northern Africa", "", true
	case "SD": // Sudan
		return "Africa", "Northern Africa", "", true
	case "TN": // Tunisia
		return "Africa", "Northern Africa", "", true
	case "EH": // Western Sahara
		return "Africa", "Northern Africa", "", true
	case "IO": // British Indian Ocean Territory
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "BI": // Burundi
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "KM": // Comoros
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "DJ": // Djibouti
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "ER": // Eritrea
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "ET": // Ethiopia
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "TF": // French Southern Territories
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "KE": // Kenya
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "MG": // Madagascar
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "MW": // Malawi
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "MU": // Mauritius
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "YT": // Mayotte
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "MZ": // Mozambique
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "RE": // Réunion
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "RW": // Rwanda
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "SC": // Seychelles
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "SO": // Somalia
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "SS": // South Sudan
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "UG": // Uganda
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "TZ": // United Republic of Tanzania
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "ZM": // Zambia
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "ZW": // Zimbabwe
		return "Africa", "Sub-Saharan Africa", "Eastern Africa", true
	case "AO": // Angola
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "CM": // Cameroon
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "CF": // Central African Republic
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "TD": // Chad
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "CG": // Congo
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "CD": // Democratic Republic of the Congo
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "GQ": // Equatorial Guinea
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "GA": // Gabon
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "ST": // Sao Tome and Principe
		return "Africa", "Sub-Saharan Africa", "Middle Africa", true
	case "BW": // Botswana
		return "Africa", "Sub-Saharan Africa", "Southern Africa", true
	case "SZ": // Eswatini
		return "Africa", "Sub-Saharan Africa", "Southern Africa", true
	case "LS": // Lesotho
		return "Africa", "Sub-Saharan Africa", "Southern Africa", true
	case "NA": // Namibia
		return "Africa", "Sub-Saharan Africa", "Southern Africa", true
	case "ZA": // South Africa
		return "Africa", "Sub-Saharan Africa", "Southern Africa", true
	case "BJ": // Benin
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "BF": // Burkina Faso
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "CV": // Cabo Verde
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "CI": // Côte d’Ivoire
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "GM": // Gambia
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "GH": // Ghana
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "GN": // Guinea
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "GW": // Guinea-Bissau
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "LR": // Liberia
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "ML": // Mali
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "MR": // Mauritania
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "NE": // Niger
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "NG": // Nigeria
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "SH": // Saint Helena
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "SN": // Senegal
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "SL": // Sierra Leone
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "TG": // Togo
		return "Africa", "Sub-Saharan Africa", "Western Africa", true
	case "AI": // Anguilla
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "AG": // Antigua and Barbuda
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "AW": // Aruba
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "BS": // Bahamas
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "BB": // Barbados
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "BQ": // Bonaire, Sint Eustatius and Saba
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "VG": // British Virgin Islands
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "KY": // Cayman Islands
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "CU": // Cuba
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "CW": // Curaçao
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "DM": // Dominica
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "DO": // Dominican Republic
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "GD": // Grenada
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "GP": // Guadeloupe
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "HT": // Haiti
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "JM": // Jamaica
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "MQ": // Martinique
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "MS": // Montserrat
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "PR": // Puerto Rico
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "BL": // Saint Barthélemy
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "KN": // Saint Kitts and Nevis
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "LC": // Saint Lucia
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "MF": // Saint Martin (French Part)
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "VC": // Saint Vincent and the Grenadines
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "SX": // Sint Maarten (Dutch part)
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "TT": // Trinidad and Tobago
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "TC": // Turks and Caicos Islands
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "VI": // United States Virgin Islands
		return "Americas", "Latin America and the Caribbean", "Caribbean", true
	case "BZ": // Belize
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "CR": // Costa Rica
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "SV": // El Salvador
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "GT": // Guatemala
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "HN": // Honduras
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "MX": // Mexico
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "NI": // Nicaragua
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "PA": // Panama
		return "Americas", "Latin America and the Caribbean", "Central America", true
	case "AR": // Argentina
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "BO": // Bolivia (Plurinational State of)
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "BV": // Bouvet Island
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "BR": // Brazil
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "CL": // Chile
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "CO": // Colombia
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "EC": // Ecuador
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "FK": // Falkland Islands (Malvinas)
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "GF": // French Guiana
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "GY": // Guyana
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "PY": // Paraguay
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "PE": // Peru
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "GS": // South Georgia and the South Sandwich Islands
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "SR": // Suriname
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "UY": // Uruguay
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "VE": // Venezuela (Bolivarian Republic of)
		return "Americas", "Latin America and the Caribbean", "South America", true
	case "BM": // Bermuda
		return "Americas", "Northern America", "", true
	case "CA": // Canada
		return "Americas", "Northern America", "", true
	case "GL": // Greenland
		return "Americas", "Northern America", "", true
	case "PM": // Saint Pierre and Miquelon
		return "Americas", "Northern America", "", true
	case "US": // United States of America
		return "Americas", "Northern America", "", true
	case "AQ": // Antarctica
		return "", "", "", true
	case "KZ": // Kazakhstan
		return "Asia", "Central Asia", "", true
	case "KG": // Kyrgyzstan
		return "Asia", "Central Asia", "", true
	case "TJ": // Tajikistan
		return "Asia", "Central Asia", "", true
	case "TM": // Turkmenistan
		return "Asia", "Central Asia", "", true
	case "UZ": // Uzbekistan
		return "Asia", "Central Asia", "", true
	case "CN": // China
		return "Asia", "Eastern Asia", "", true
	case "HK": // China, Hong Kong Special Administrative Region
		return "Asia", "Eastern Asia", "", true
	case "MO": // China, Macao Special Administrative Region
		return "Asia", "Eastern Asia", "", true
	case "KP": // Democratic People's Republic of Korea
		return "Asia", "Eastern Asia", "", true
	case "JP": // Japan
		return "Asia", "Eastern Asia", "", true
	case "MN": // Mongolia
		return "Asia", "Eastern Asia", "", true
	case "KR": // Republic of Korea
		return "Asia", "Eastern Asia", "", true
	case "BN": // Brunei Darussalam
		return "Asia", "South-eastern Asia", "", true
	case "KH": // Cambodia
		return "Asia", "South-eastern Asia", "", true
	case "ID": // Indonesia
		return "Asia", "South-eastern Asia", "", true
	case "LA": // Lao People's Democratic Republic
		return "Asia", "South-eastern Asia", "", true
	case "MY": // Malaysia
		return "Asia", "South-eastern Asia", "", true
	case "MM": // Myanmar
		return "Asia", "South-eastern Asia", "", true
	case "PH": // Philippines
		return "Asia", "South-eastern Asia", "", true
	case "SG": // Singapore
		return "Asia", "South-eastern Asia", "", true
	case "TH": // Thailand
		return "Asia", "South-eastern Asia", "", true
	case "TL": // Timor-Leste
		return "Asia", "South-eastern Asia", "", true
	case "VN": // Viet Nam
		return "Asia", "South-eastern Asia", "", true
	case "AF": // Afghanistan
		return "Asia", "Southern Asia", "", true
	case "BD": // Bangladesh
		return "Asia", "Southern Asia", "", true
	case "BT": // Bhutan
		return "Asia", "Southern Asia", "", true
	case "IN": // India
		return "Asia", "Southern Asia", "", true
	case "IR": // Iran (Islamic Republic of)
		return "Asia", "Southern Asia", "", true
	case "MV": // Maldives
		return "Asia", "Southern Asia", "", true
	case "NP": // Nepal
		return "Asia", "Southern Asia", "", true
	case "PK": // Pakistan
		return "Asia", "Southern Asia", "", true
	case "LK": // Sri Lanka
		return "Asia", "Southern Asia", "", true
	case "AM": // Armenia
		return "Asia", "Western Asia", "", true
	case "AZ": // Azerbaijan
		return "Asia", "Western Asia", "", true
	case "BH": // Bahrain
		return "Asia", "Western Asia", "", true
	case "CY": // Cyprus
		return "Asia", "Western Asia", "", true
	case "GE": // Georgia
		return "Asia", "Western Asia", "", true
	case "IQ": // Iraq
		return "Asia", "Western Asia", "", true
	case "IL": // Israel
		return "Asia", "Western Asia", "", true
	case "JO": // Jordan
		return "Asia", "Western Asia", "", true
	case "KW": // Kuwait
		return "Asia", "Western Asia", "", true
	case "LB": // Lebanon
		return "Asia", "Western Asia", "", true
	case "OM": // Oman
		return "Asia", "Western Asia", "", true
	case "QA": // Qatar
		return "Asia", "Western Asia", "", true
	case "SA": // Saudi Arabia
		return "Asia", "Western Asia", "", true
	case "PS": // State of Palestine
		return "Asia", "Western Asia", "", true
	case "SY": // Syrian Arab Republic
		return "Asia", "Western Asia", "", true
	case "TR": // Türkiye
		return "Asia", "Western Asia", "", true
	case "AE": // United Arab Emirates
		return "Asia", "Western Asia", "", true
	case "YE": // Yemen
		return "Asia", "Western Asia", "", true
	case "BY": // Belarus
		return "Europe", "Eastern Europe", "", true
	case "BG": // Bulgaria
		return "Europe", "Eastern Europe", "", true
	case "CZ": // Czechia
		return "Europe", "Eastern Europe", "", true
	case "HU": // Hungary
		return "Europe", "Eastern Europe", "", true
	case "PL": // Poland
		return "Europe", "Eastern Europe", "", true
	case "MD": // Republic of Moldova
		return "Europe", "Eastern Europe", "", true
	case "RO": // Romania
		return "Europe", "Eastern Europe", "", true
	case "RU": // Russian Federation
		return "Europe", "Eastern Europe", "", true
	case "SK": // Slovakia
		return "Europe", "Eastern Europe", "", true
	case "UA": // Ukraine
		return "Europe", "Eastern Europe", "", true
	case "AX": // Åland Islands
		return "Europe", "Northern Europe", "", true
	case "GG": // Guernsey
		return "Europe", "Northern Europe", "Channel Islands", true
	case "JE": // Jersey
		return "Europe", "Northern Europe", "Channel Islands", true
	case "": // Sark
		return "Europe", "Northern Europe", "Channel Islands", true
	case "DK": // Denmark
		return "Europe", "Northern Europe", "", true
	case "EE": // Estonia
		return "Europe", "Northern Europe", "", true
	case "FO": // Faroe Islands
		return "Europe", "Northern Europe", "", true
	case "FI": // Finland
		return "Europe", "Northern Europe", "", true
	case "IS": // Iceland
		return "Europe", "Northern Europe", "", true
	case "IE": // Ireland
		return "Europe", "Northern Europe", "", true
	case "IM": // Isle of Man
		return "Europe", "Northern Europe", "", true
	case "LV": // Latvia
		return "Europe", "Northern Europe", "", true
	case "LT": // Lithuania
		return "Europe", "Northern Europe", "", true
	case "NO": // Norway
		return "Europe", "Northern Europe", "", true
	case "SJ": // Svalbard and Jan Mayen Islands
		return "Europe", "Northern Europe", "", true
	case "SE": // Sweden
		return "Europe", "Northern Europe", "", true
	case "GB": // United Kingdom of Great Britain and Northern Ireland
		return "Europe", "Northern Europe", "", true
	case "AL": // Albania
		return "Europe", "Southern Europe", "", true
	case "AD": // Andorra
		return "Europe", "Southern Europe", "", true
	case "BA": // Bosnia and Herzegovina
		return "Europe", "Southern Europe", "", true
	case "HR": // Croatia
		return "Europe", "Southern Europe", "", true
	case "GI": // Gibraltar
		return "Europe", "Southern Europe", "", true
	case "GR": // Greece
		return "Europe", "Southern Europe", "", true
	case "VA": // Holy See
		return "Europe", "Southern Europe", "", true
	case "IT": // Italy
		return "Europe", "Southern Europe", "", true
	case "MT": // Malta
		return "Europe", "Southern Europe", "", true
	case "ME": // Montenegro
		return "Europe", "Southern Europe", "", true
	case "MK": // North Macedonia
		return "Europe", "Southern Europe", "", true
	case "PT": // Portugal
		return "Europe", "Southern Europe", "", true
	case "SM": // San Marino
		return "Europe", "Southern Europe", "", true
	case "RS": // Serbia
		return "Europe", "Southern Europe", "", true
	case "SI": // Slovenia
		return "Europe", "Southern Europe", "", true
	case "ES": // Spain
		return "Europe", "Southern Europe", "", true
	case "AT": // Austria
		return "Europe", "Western Europe", "", true
	case "BE": // Belgium
		return "Europe", "Western Europe", "", true
	case "FR": // France
		return "Europe", "Western Europe", "", true
	case "DE": // Germany
		return "Europe", "Western Europe", "", true
	case "LI": // Liechtenstein
		return "Europe", "Western Europe", "", true
	case "LU": // Luxembourg
		return "Europe", "Western Europe", "", true
	case "MC": // Monaco
		return "Europe", "Western Europe", "", true
	case "NL": // Netherlands
		return "Europe", "Western Europe", "", true
	case "CH": // Switzerland
		return "Europe", "Western Europe", "", true
	case "AU": // Australia
		return "Oceania", "Australia and New Zealand", "", true
	case "CX": // Christmas Island
		return "Oceania", "Australia and New Zealand", "", true
	case "CC": // Cocos (Keeling) Islands
		return "Oceania", "Australia and New Zealand", "", true
	case "HM": // Heard Island and McDonald Islands
		return "Oceania", "Australia and New Zealand", "", true
	case "NZ": // New Zealand
		return "Oceania", "Australia and New Zealand", "", true
	case "NF": // Norfolk Island
		return "Oceania", "Australia and New Zealand", "", true
	case "FJ": // Fiji
		return "Oceania", "Melanesia", "", true
	case "NC": // New Caledonia
		return "Oceania", "Melanesia", "", true
	case "PG": // Papua New Guinea
		return "Oceania", "Melanesia", "", true
	case "SB": // Solomon Islands
		return "Oceania", "Melanesia", "", true
	case "VU": // Vanuatu
		return "Oceania", "Melanesia", "", true
	case "GU": // Guam
		return "Oceania", "Micronesia", "", true
	case "KI": // Kiribati
		return "Oceania", "Micronesia", "", true
	case "MH": // Marshall Islands
		return "Oceania", "Micronesia", "", true
	case "FM": // Micronesia (Federated States of)
		return "Oceania", "Micronesia", "", true
	case "NR": // Nauru
		return "Oceania", "Micronesia", "", true
	case "MP": // Northern Mariana Islands
		return "Oceania", "Micronesia", "", true
	case "PW": // Palau
		return "Oceania", "Micronesia", "", true
	case "UM": // United States Minor Outlying Islands
		return "Oceania", "Micronesia", "", true
	case "AS": // American Samoa
		return "Oceania", "Polynesia", "", true
	case "CK": // Cook Islands
		return "Oceania", "Polynesia", "", true
	case "PF": // French Polynesia
		return "Oceania", "Polynesia", "", true
	case "NU": // Niue
		return "Oceania", "Polynesia", "", true
	case "PN": // Pitcairn
		return "Oceania", "Polynesia", "", true
	case "WS": // Samoa
		return "Oceania", "Polynesia", "", true
	case "TK": // Tokelau
		return "Oceania", "Polynesia", "", true
	case "TO": // Tonga
		return "Oceania", "Polynesia", "", true
	case "TV": // Tuvalu
		return "Oceania", "Polynesia", "", true
	case "WF": // Wallis and Futuna Islands
		return "Oceania", "Polynesia", "", true
	}
	return "", "", "", false
}
