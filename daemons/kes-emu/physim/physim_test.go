package physim

import (
	"math"
	"testing"
	"time"
)

func TestPhysimDayNight(t *testing.T) {
	loc := time.Local
	night := time.Date(2026, 1, 1, 1, 0, 0, 0, loc)
	noon := time.Date(2026, 1, 1, 12, 0, 0, 0, loc)
	m := New(Default(), night)
	m.Tick(night)
	sn := m.Snapshot()
	m.Tick(noon)
	sd := m.Snapshot()

	if sd.PPV <= sn.PPV {
		t.Fatalf("днём PPV (%v) должно быть больше ночного (%v)", sd.PPV, sn.PPV)
	}
	if sn.PPV < 0 || sd.PPV > Default().PanelPmax*1.05 {
		t.Fatalf("PPV вне диапазона: ночь %v, полдень %v", sn.PPV, sd.PPV)
	}
}

func TestPhysimCells(t *testing.T) {
	loc := time.Local
	m := New(Default(), time.Date(2026, 1, 1, 0, 0, 0, 0, loc))
	for h := 0; h < 24; h++ {
		m.Tick(time.Date(2026, 1, 1, h, 0, 0, 0, loc))
		s := m.Snapshot()
		if len(s.Ucell) != 16 {
			t.Fatalf("ячеек %d, ожидалось 16", len(s.Ucell))
		}
		lo, hi := s.Ucell[0], s.Ucell[0]
		for _, u := range s.Ucell {
			if u < 3.10 || u > 3.65 {
				t.Fatalf("Ucell=%v вне 3.10..3.65", u)
			}
			if u < lo {
				lo = u
			}
			if u > hi {
				hi = u
			}
		}
		if hi-lo > 0.03 {
			t.Fatalf("разброс ячеек слишком большой: %v", hi-lo)
		}
	}
}

func TestPhysimSOCSmooth(t *testing.T) {
	loc := time.Local
	m := New(Default(), time.Date(2026, 1, 1, 8, 0, 0, 0, loc))
	prev := m.Snapshot().SOC
	for i := 1; i <= 8*60; i++ { // 8 ч по минуте
		m.Tick(time.Date(2026, 1, 1, 8, i, 0, 0, loc))
		s := m.Snapshot()
		if math.Abs(s.SOC-prev) > 0.01 { // плавно (за минуту — доли процента)
			t.Fatalf("скачок SOC за минуту: %v -> %v", prev, s.SOC)
		}
		prev = s.SOC
	}
}

func TestPhysimClimate(t *testing.T) {
	loc := time.Local
	janNight := time.Date(2026, 1, 15, 4, 0, 0, 0, loc)
	janNoon := time.Date(2026, 1, 15, 15, 0, 0, 0, loc)
	julNoon := time.Date(2026, 7, 15, 15, 0, 0, 0, loc)
	m := New(Default(), janNight)
	if m.ambientTemp(janNight) >= m.ambientTemp(janNoon) {
		t.Fatalf("январь: ночь (%v) должна быть холоднее дня (%v)", m.ambientTemp(janNight), m.ambientTemp(janNoon))
	}
	if m.ambientTemp(janNight) >= -5 || m.ambientTemp(janNight) < -15 {
		t.Fatalf("январь ночь (%v) вне -15..-5", m.ambientTemp(janNight))
	}
	if julNoonSub := m.ambientTemp(julNoon); julNoonSub-m.ambientTemp(janNoon) < 15 {
		t.Fatalf("июль (%v) должен быть заметно теплее января (%v)", julNoonSub, m.ambientTemp(janNoon))
	}
	if m.ambientTemp(julNoon) < 20 || m.ambientTemp(julNoon) > 35 {
		t.Fatalf("июль день (%v) вне 20..35", m.ambientTemp(julNoon))
	}
}
