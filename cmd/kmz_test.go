package cmd

import "testing"

func TestDelta(t *testing.T) {
	// this is a critical fcn that must work for any rectangular
	// chunk of the world, possibly spanning the 0 long and 0 lat.
	deltaT(t, 100, 100, [4]float64{50, 40, 10, 0}, 10000, 10000)
	deltaT(t, 1000, 1000, [4]float64{-50, -60, 10, 0}, 10000, 10000)
	deltaT(t, 1000, 1000, [4]float64{50, 40, 0, -10}, 10000, 10000)
	deltaT(t, 1000, 1000, [4]float64{50, 40, -120, -130}, 10000, 10000)
	deltaT(t, 1000, 1000, [4]float64{50, 40, -170, 170}, 10000, 10000)

}

func deltaT(t *testing.T, tileWidth, tileHeight int, box [4]float64, totWidth, totHeight int) {

	var tbox = [4]float64{0, 0, 0, 0}
	tbox[north] = box[north]
	tbox[west] = box[west]
	widthSum := 0
	for i := 0; i < 100; i++ {
		ns, ew := delta(tileWidth, tileHeight, box, totWidth, totHeight)
		tbox[east] = tbox[west] + ew
		if tbox[east] > 180 {
			tbox[east] = tbox[east] - 360
		}
		tbox[south] = tbox[north] - ns

		if tbox[east] < -180 || tbox[east] > 180 {
			t.Errorf("E(%v) not in [-180,180]", tbox[east])
		}
		if tbox[west] < -180 || tbox[west] > 180 {
			t.Errorf("W(%v) not in [-180,180]", tbox[west])
		}
		if tbox[north] < -90 || tbox[north] > 90 {
			t.Errorf("N(%v) not in [-90,90]", tbox[north])
		}
		if tbox[south] < -90 || tbox[south] > 90 {
			t.Errorf("N(%v) not in [-90,90]", tbox[south])
		}

		if tbox[north] < tbox[south] {
			t.Errorf("T1: N(%v) < S(%v) ", tbox[north], tbox[south])
		}

		if widthSum >= totWidth {
			// drop down a row
			tbox[north] = tbox[south]
			tbox[west] = box[west]
			widthSum = 0
		} else {
			tbox[west] = tbox[east]
		}
	}
}

func TestEWD(t *testing.T) {
	vals := []struct{ east, west, delta float64 }{
		{10, 0, 10},
		{-100, -120, 20},
		{10, -10, 20},
		{-170, 170, 20},
		{170, 178, 352},
	}
	for _, v := range vals {
		if eastDelta(v.east, v.west) != v.delta {
			t.Errorf("Wrong EW delta: %v, val: %v", eastDelta(v.east, v.west), v)
		}
	}
}

func TestNorm(t *testing.T) {
	vals := []struct{ deg, norm float64 }{
		{10, 10},
		{-100, -100},
		{-170, -170},
		{180, 180},
		{0, 0},
		{-185, 175},
		{-180, -180},
		{-360, 0},
		{360, 0},
		{420, 60},
		{-420, -60},
	}
	for _, v := range vals {
		if normEasting(v.deg) != v.norm {
			t.Errorf("Wrong norm: %v, val: %v", normEasting(v.deg), v)
		}
	}
}
