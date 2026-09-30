package modelregistry

import (
	"errors"
	"sync"
	"testing"
)

type testModel struct{ ID int }

type recursivePtr *recursivePtr

func TestSentinelErrors(t *testing.T) {
	r := NewModelRegistry()
	if _, err := r.GetModel("nope"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("GetModel: want ErrModelNotFound, got %v", err)
	}
	if _, err := r.GetModelRules("nope"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("GetModelRules: want ErrModelNotFound, got %v", err)
	}
	if err := r.SetModelRules("nope", ModelRules{}); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("SetModelRules: want ErrModelNotFound, got %v", err)
	}
	if err := r.RegisterModel("a", testModel{}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterModel("a", testModel{}); !errors.Is(err, ErrModelExists) {
		t.Fatalf("want ErrModelExists, got %v", err)
	}
	if err := r.RegisterModel("b", nil); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("want ErrInvalidModel, got %v", err)
	}
	if err := r.RegisterModel("c", 42); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("want ErrInvalidModel, got %v", err)
	}
}

func TestRecursivePointerTypeRejected(t *testing.T) {
	var x recursivePtr
	if err := NewModelRegistry().RegisterModel("r", x); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("want ErrInvalidModel, got %v", err)
	}
}

func TestPointerNormalised(t *testing.T) {
	r := NewModelRegistry()
	if err := r.RegisterModel("p", &testModel{}); err != nil {
		t.Fatal(err)
	}
	m, _ := r.GetModel("p")
	if _, ok := m.(testModel); !ok {
		t.Fatalf("want testModel value, got %T", m)
	}
}

// Rules must never be observable as permissive for a restrictively registered model.
func TestRegisterModelWithRulesAtomic(t *testing.T) {
	for i := 0; i < 200; i++ {
		r := NewModelRegistry()
		var wg sync.WaitGroup
		stop := make(chan struct{})
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if rules, err := r.GetModelRules("m"); err == nil && rules.CanDelete {
					t.Error("observed permissive rules for restrictive model")
					return
				}
			}
		}()
		if err := r.RegisterModelWithRules("m", testModel{}, ModelRules{CanRead: true}); err != nil {
			t.Fatal(err)
		}
		close(stop)
		wg.Wait()
	}
}

func TestIterateModelsCallbackMayReenter(t *testing.T) {
	prev := GetDefaultRegistry()
	reg := NewModelRegistry()
	SetDefaultRegistry(reg)
	defer SetDefaultRegistry(prev)

	if err := RegisterModel(testModel{}, "iter.a"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		IterateModels(func(name string, _ interface{}) {
			_ = RegisterModel(testModel{}, name+".copy") // would deadlock if lock held
		})
	}()
	<-done
	if _, err := reg.GetModel("iter.a.copy"); err != nil {
		t.Fatal(err)
	}
}

func TestGetModelRulesByNameAcrossRegistries(t *testing.T) {
	extra := NewModelRegistry()
	if err := extra.RegisterModelWithRules("x.only", testModel{}, ModelRules{CanRead: true}); err != nil {
		t.Fatal(err)
	}
	AddRegistry(extra)
	rules, err := GetModelRulesByName("x.only")
	if err != nil || rules.CanDelete {
		t.Fatalf("rules=%+v err=%v", rules, err)
	}
	if _, err := GetModelRulesByName("x.missing"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("want ErrModelNotFound, got %v", err)
	}
}

func TestConcurrentAccessRace(t *testing.T) {
	r := NewModelRegistry()
	_ = r.RegisterModel("seed", testModel{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = r.SetModelRules("seed", ModelRules{CanRead: j%2 == 0})
				_, _ = r.GetModelRules("seed")
				_ = r.GetAllModels()
				_ = GetDefaultRegistry()
				_ = GetModels()
			}
		}(i)
	}
	wg.Wait()
}

func TestIterateModelsRecoversCallbackPanic(t *testing.T) {
	prev := GetDefaultRegistry()
	SetDefaultRegistry(NewModelRegistry())
	defer SetDefaultRegistry(prev)

	_ = RegisterModel(testModel{}, "p.a")
	_ = RegisterModel(testModel{}, "p.b")
	calls := 0
	IterateModels(func(string, interface{}) {
		calls++
		panic("boom")
	})
	if calls != 2 {
		t.Fatalf("want both models visited, got %d", calls)
	}
	// Registry must still be usable (no lock left held).
	if err := RegisterModel(testModel{}, "p.c"); err != nil {
		t.Fatal(err)
	}
}
