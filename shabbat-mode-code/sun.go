package main

import (
	"math"
	"time"
)

const rad = math.Pi / 180

// sunsetUTC computes sea-level sunset (NOAA method) for a calendar date.
func sunsetUTC(y int, m time.Month, d int, lat, lon float64) time.Time {
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	jd := float64(midnight.Unix())/86400 + 2440587.5
	minutes := 720.0 - 4*lon // first guess: solar noon
	for i := 0; i < 4; i++ {
		t := (jd + minutes/1440 - 2451545) / 36525
		l0 := math.Mod(280.46646+t*(36000.76983+t*0.0003032), 360)
		ma := 357.52911 + t*(35999.05029-0.0001537*t)
		e := 0.016708634 - t*(0.000042037+0.0000001267*t)
		c := math.Sin(ma*rad)*(1.914602-t*(0.004817+0.000014*t)) +
			math.Sin(2*ma*rad)*(0.019993-0.000101*t) + math.Sin(3*ma*rad)*0.000289
		omega := 125.04 - 1934.136*t
		lambda := l0 + c - 0.00569 - 0.00478*math.Sin(omega*rad)
		e0 := 23 + (26+(21.448-t*(46.815+t*(0.00059-t*0.001813)))/60)/60
		obl := e0 + 0.00256*math.Cos(omega*rad)
		decl := math.Asin(math.Sin(obl*rad)*math.Sin(lambda*rad)) / rad
		yy := math.Tan(obl/2*rad) * math.Tan(obl/2*rad)
		eq := 4 / rad * (yy*math.Sin(2*l0*rad) - 2*e*math.Sin(ma*rad) +
			4*e*yy*math.Sin(ma*rad)*math.Cos(2*l0*rad) -
			0.5*yy*yy*math.Sin(4*l0*rad) - 1.25*e*e*math.Sin(2*ma*rad))
		ha := math.Acos(math.Cos(90.833*rad)/(math.Cos(lat*rad)*math.Cos(decl*rad))-
			math.Tan(lat*rad)*math.Tan(decl*rad)) / rad
		minutes = 720 - 4*lon - eq + 4*ha
	}
	return midnight.Add(time.Duration(minutes * float64(time.Minute)))
}
