package service

import (
	"sort"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

func (s *Service) CreateFormula(
	input CreateFormulaInput,
) (InstructionResult[domain.Formula], error) {
	now := s.clock.Now()
	formula := domain.Formula{
		Name:                  input.Name,
		Style:                 input.Style,
		TargetOriginalGravity: input.TargetOriginalGravity,
		TargetFinalGravity:    input.TargetFinalGravity,
		MaxFermentationTempC:  input.MaxFermentationTempC,
		MinimumDays:           input.MinimumDays,
		MaximumDays:           input.MaximumDays,
		Steps:                 []domain.FormulaStep{},
		State:                 domain.FormulaDraft,
		Version:               1,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	formula.Normalize()
	if err := formula.Validate(); err != nil {
		return InstructionResult[domain.Formula]{}, err
	}
	id, err := generateEntityID("formula")
	if err != nil {
		return InstructionResult[domain.Formula]{},
			domain.NewError(domain.CodeStorage, err.Error())
	}
	formula.ID = id
	nameKey := domain.NameKey(formula.Name)

	return runInstruction(s, input.Instruction, 201,
		func(snapshot *domain.Snapshot) (domain.Formula, error) {
			if _, exists := snapshot.NameIndex[nameKey]; exists {
				return domain.Formula{}, domain.NewError(domain.CodeFormulaNameConflict,
					"a formula with the same normalized name already exists")
			}
			snapshot.Formulas[formula.ID] = formula
			snapshot.NameIndex[nameKey] = formula.ID
			return formula.Clone(), nil
		})
}

func (s *Service) SetFormulaSteps(
	id string,
	input SetFormulaStepsInput,
) (InstructionResult[domain.Formula], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (domain.Formula, error) {
			formula, ok := snapshot.Formulas[id]
			if !ok {
				return domain.Formula{}, domain.NewError(domain.CodeFormulaNotFound,
					"formula was not found")
			}
			if err := s.checkExpected(formula.Version, input.ExpectedVersion); err != nil {
				return domain.Formula{}, err
			}
			if err := formula.SetSteps(input.Steps); err != nil {
				return domain.Formula{}, err
			}
			formula.UpdatedAt = s.clock.Now()
			snapshot.Formulas[id] = formula
			return formula.Clone(), nil
		})
}

func (s *Service) ApproveFormula(
	id string,
	input VersionInput,
) (InstructionResult[domain.Formula], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (domain.Formula, error) {
			formula, ok := snapshot.Formulas[id]
			if !ok {
				return domain.Formula{}, domain.NewError(domain.CodeFormulaNotFound,
					"formula was not found")
			}
			if err := s.checkExpected(formula.Version, input.ExpectedVersion); err != nil {
				return domain.Formula{}, err
			}
			if err := formula.Approve(s.clock.Now()); err != nil {
				return domain.Formula{}, err
			}
			snapshot.Formulas[id] = formula
			return formula.Clone(), nil
		})
}

func (s *Service) RetireFormula(
	id string,
	input VersionInput,
) (InstructionResult[domain.Formula], error) {
	return runInstruction(s, input.Instruction, 200,
		func(snapshot *domain.Snapshot) (domain.Formula, error) {
			formula, ok := snapshot.Formulas[id]
			if !ok {
				return domain.Formula{}, domain.NewError(domain.CodeFormulaNotFound,
					"formula was not found")
			}
			if err := s.checkExpected(formula.Version, input.ExpectedVersion); err != nil {
				return domain.Formula{}, err
			}
			for _, run := range snapshot.Runs {
				if run.FormulaID == id && run.State != domain.RunCompleted &&
					run.State != domain.RunAborted {
					return domain.Formula{}, domain.NewError(domain.CodeFormulaInUse,
						"formula is attached to a run that is still open")
				}
			}
			if err := formula.Retire(s.clock.Now()); err != nil {
				return domain.Formula{}, err
			}
			snapshot.Formulas[id] = formula
			return formula.Clone(), nil
		})
}

func (s *Service) GetFormula(id string) (domain.Formula, error) {
	var found domain.Formula
	err := s.store.View(func(snapshot domain.Snapshot) error {
		formula, ok := snapshot.Formulas[id]
		if !ok {
			return domain.NewError(domain.CodeFormulaNotFound, "formula was not found")
		}
		found = formula.Clone()
		return nil
	})
	if err != nil {
		return domain.Formula{}, err
	}
	return found, nil
}

func (s *Service) ListFormulas() ([]domain.Formula, error) {
	var formulas []domain.Formula
	err := s.store.View(func(snapshot domain.Snapshot) error {
		formulas = make([]domain.Formula, 0, len(snapshot.Formulas))
		for _, formula := range snapshot.Formulas {
			formulas = append(formulas, formula.Clone())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(formulas, func(i, j int) bool {
		if formulas[i].Name == formulas[j].Name {
			return formulas[i].ID < formulas[j].ID
		}
		return formulas[i].Name < formulas[j].Name
	})
	return formulas, nil
}
