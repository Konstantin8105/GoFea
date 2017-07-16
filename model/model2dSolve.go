package model

import (
	"fmt"
	"math"

	"github.com/Konstantin8105/GoFea/dof"
	"github.com/Konstantin8105/GoFea/element"
	"github.com/Konstantin8105/GoFea/finiteElement"
	"github.com/Konstantin8105/GoFea/utils"
	"github.com/Konstantin8105/GoLinAlg/linAlg"
	"github.com/Konstantin8105/GoLinAlg/linAlg/solver"
)

// Solve - solving finite element
func (m *Dim2) Solve() (err error) {

	for caseNumber := 0; caseNumber < len(m.forceCases); caseNumber++ {

		// TODO : check everything
		// TODO : sort  everything
		// TODO : compress loads by number

		// Generate degree of freedom in global system
		var degreeGlobal []dof.AxeNumber
		dofSystem := dof.NewBeam(m.beams, dof.Dim2d)
		for _, beam := range m.beams {
			fe := m.getBeamFiniteElement(beam.Index)
			_, degreeLocal := finiteElement.GetStiffinerGlobalK(fe, &dofSystem, finiteElement.WithoutZeroStiffiner)
			degreeGlobal = append(degreeGlobal, degreeLocal...)
		}
		{
			is := dof.ConvertToInt(degreeGlobal)
			utils.UniqueInt(&is)
			degreeGlobal = dof.ConvertToAxe(is)
		}

		// Create convertor index to axe
		mapIndex := dof.NewMapIndex(&degreeGlobal)

		// Generate global stiffiner matrix [Ko]
		stiffinerKGlobal := m.convertFromLocalToGlobalSystem(&degreeGlobal, &dofSystem, &mapIndex, finiteElement.GetStiffinerGlobalK)

		// Create load vector
		loads := linAlg.NewMatrix64bySize(len(degreeGlobal), 1)
		for _, node := range m.forceCases[caseNumber].nodeForces {
			for _, inx := range node.pointIndexes {
				d := dofSystem.GetDoF(inx)
				if node.nodeForce.Fx != 0.0 {
					h, err := mapIndex.GetByAxe(d[0])
					if err == nil {
						loads.Set(h, 0, node.nodeForce.Fx)
					}
				}
				if node.nodeForce.Fy != 0.0 {
					h, err := mapIndex.GetByAxe(d[1])
					if err == nil {
						loads.Set(h, 0, node.nodeForce.Fy)
					}
				}
				if node.nodeForce.M != 0.0 {
					h, err := mapIndex.GetByAxe(d[2])
					if err == nil {
						loads.Set(h, 0, node.nodeForce.M)
					}
				}
			}
		}

		// Create array degree for support
		// and modify the global stiffiner matrix
		// and load vector
		for _, sup := range m.supports {
			for _, inx := range sup.pointIndexes {
				d := dofSystem.GetDoF(inx)
				var result []dof.AxeNumber
				if sup.support.Dx == true {
					result = append(result, d[0])
				}
				if sup.support.Dy == true {
					result = append(result, d[1])
				}
				if sup.support.M == true {
					result = append(result, d[2])
				}
				// modify stiffiner matrix for correct
				// adding support
				for i := 0; i < len(result); i++ {
					g, err := mapIndex.GetByAxe(result[i])
					if err != nil {
						continue
					}
					for j := 0; j < len(degreeGlobal); j++ {
						h, err := mapIndex.GetByAxe(degreeGlobal[j])
						if err != nil {
							continue
						}
						stiffinerKGlobal.Set(g, h, 0.0)
						stiffinerKGlobal.Set(h, g, 0.0)
					}
					stiffinerKGlobal.Set(g, g, 1.0)
					// modify load vector on support
					loads.Set(g, 0, 0.0)
				}
			}
		}
		//fmt.Println("degreeGlobal = ", degreeGlobal)
		//fmt.Printf("K global = \n%s\n", stiffinerKGlobal)
		//fmt.Printf("Load vector = \n%s\n", loads)

		// Solving system of linear equations for finding
		// the displacement in points in global system
		// TODO: if you have nonlinear elements, then we can use
		// TODO: one global stiffiner matrix for all cases
		lu := solver.NewLUsolver(stiffinerKGlobal)
		x := lu.Solve(loads)
		// TODO: rename global vector of displacement

		//fmt.Printf("Global displacement = \n%s\n", x)
		//fmt.Println("degreeGlobal = ", degreeGlobal)
		for _, beam := range m.beams {
			fe := m.getBeamFiniteElement(beam.Index)
			/*klocal,*/ _, degreeLocal := finiteElement.GetStiffinerGlobalK(fe, &dofSystem, finiteElement.FullInformation)
			//fmt.Println("=============")
			//fmt.Println("klocalGlobal = ", klocal)
			//fmt.Println("degreeLocal = ", degreeLocal)
			globalDisplacement := make([]float64, len(degreeLocal))
			// if not found in global displacement, then it is a pinned
			// in local stiffiner matrix - than row and column is zero
			// for avoid collisian - we put a zero
			for i := 0; i < len(globalDisplacement); i++ {
				for j := 0; j < len(degreeGlobal); j++ {
					if degreeLocal[i] == degreeGlobal[j] {
						globalDisplacement[i] = x.Get(j, 0)
						break
					}
				}
			}
			//fmt.Println("globalDisplacement = ", globalDisplacement)

			t := linAlg.NewMatrix64bySize(10, 10)
			fe.GetCoordinateTransformation(&t)
			//fmt.Println("tr.glo --", t)

			// Zo = T_t * Z
			var localDisplacement []float64
			for i := 0; i < t.GetRowSize(); i++ {
				sum := 0.0
				for j := 0; j < t.GetColumnSize(); j++ {
					sum += t.Get(i, j) * globalDisplacement[j]
				}
				localDisplacement = append(localDisplacement, sum)
			}
			//fmt.Println("localDisplacement = ", localDisplacement)

			kk := linAlg.NewMatrix64bySize(10, 10)
			fe.GetStiffinerK(&kk)
			//fmt.Println("klocalll -->", kk)

			var localForce []float64
			for i := 0; i < kk.GetRowSize(); i++ {
				sum := 0.0
				for j := 0; j < kk.GetRowSize(); j++ {
					sum += kk.Get(i, j) * localDisplacement[j]
				}
				localForce = append(localForce, sum)
			}
			//fmt.Println("localForce = ", localForce)
			_ = localForce
		}

		//TODO: can calculated in parallel local force

		// Generate global mass matrix [Mo]
		n := stiffinerKGlobal.GetRowSize()
		massGlobal := m.convertFromLocalToGlobalSystem(&degreeGlobal, &dofSystem, &mapIndex, finiteElement.GetGlobalMass)
		// m.convertFromLocalToGlobalSystem(&degreeGlobal, &dofSystem, &mapIndex, finiteElement.GetGlobalMass)
		//  linAlg.NewMatrix64bySize(n, n)

		// TODO: Add to matrix mass the nodal mass
		for _, node := range m.forceCases[caseNumber].nodeForces {
			for _, inx := range node.pointIndexes {
				d := dofSystem.GetDoF(inx)
				if node.nodeForce.Fx != 0.0 {
					h, err := mapIndex.GetByAxe(d[0])
					if err == nil {
						massGlobal.Set(h, h, massGlobal.Get(h, h)+math.Abs(node.nodeForce.Fx))
					}
				}
				if node.nodeForce.Fy != 0.0 {
					h, err := mapIndex.GetByAxe(d[1])
					if err == nil {
						massGlobal.Set(h, h, massGlobal.Get(h, h)+math.Abs(node.nodeForce.Fy))
					}
				}
				// TODO: Moment haven`t mass ???
				// TODO: Check
				//if node.nodeForce.M != 0.0 {
				//	h, err := mapIndex.GetByAxe(d[2])
				//	if err == nil {
				//		massGlobal.Set(h, h, massGlobal.Get(h, h)+math.Abs(node.nodeForce.M))
				//		fmt.Println("Add M to mass")
				//	}
				//}
			}
		}

		//TODO: CHECKUING GRAVITY TO MATRIX MASS
		for i := 0; i < massGlobal.GetRowSize(); i++ {
			for j := 0; j < massGlobal.GetColumnSize(); j++ {
				massGlobal.Set(i, j, massGlobal.Get(i, j)/9.806)
			}
		}
		// TODO: ADD to mass WITH OR WITOUT SELFWEIGHT

		// Calculate matrix [H] = [Ko]^-1 * [Mo]
		if stiffinerKGlobal.GetRowSize() != stiffinerKGlobal.GetColumnSize() {
			panic("Not correct size of global stiffiner matrix")
		}
		//fmt.Println("GlobalMass = ", massGlobal)
		Ho := linAlg.NewMatrix64bySize(n, n)
		buffer := linAlg.NewMatrix64bySize(n, 1)
		for i := 0; i < n; i++ {
			// Create vertical vector from [Mo]
			for j := 0; j < n; j++ {
				buffer.Set(j, 0, massGlobal.Get(j, i))
			}
			// Calculation
			result := lu.Solve(buffer)
			// Add vector to [Ho]
			for j := 0; j < n; j++ {
				Ho.Set(j, i, result.Get(j, 0))
			}
		}
		//fmt.Println("[Ho] = ", Ho)
		{
			// TODO: check
			// Remove zero rows and columns
			var removePosition []int
			// TODO: len --> to matrix lenght
			// TODO: at the first check diagonal element
			for i := 0; i < Ho.GetRowSize(); i++ {
				found := false
				for j := 0; j < Ho.GetRowSize(); j++ {
					if Ho.Get(i, j) != 0.0 {
						found = true
						break
					}
				}
				if found {
					continue
				}
				removePosition = append(removePosition, i)
			}
			Ho.RemoveRowAndColumn(removePosition...)
		}

		// Calculation of natural frequency
		eigen := solver.NewEigen(Ho)
		//fmt.Println("lambda       = ", eigen.GetRealEigenvalues())
		//fmt.Println("eigenvectors = ", eigen.GetV())
		//fmt.Println("getD = ", eigen.GetD())

		// TODO: fix for avoid strange frequency some is too small or too big
		value := eigen.GetRealEigenvalues()
		for _, v := range value {
			freq := math.Sqrt(1.0/v) / 2.0 / math.Pi
			fmt.Printf("f = %.5v Hz\n", freq)
			_ = freq
		}
		// TODO: need add modal mass values for natural frquency calculation

		// Linear buckling
		//potentialGlobal := m.convertFromLocalToGlobalSystem(&degreeGlobal, &dofSystem, &mapIndex, finiteElement.GetGlobalPotential)
		potentialGlobal := linAlg.NewMatrix64bySize(stiffinerKGlobal.GetRowSize(), stiffinerKGlobal.GetColumnSize())
		for _, beam := range m.beams {
			fe := m.getBeamFiniteElement(beam.Index)

			/*klocal,*/
			_, degreeLocal := finiteElement.GetStiffinerGlobalK(fe, &dofSystem, finiteElement.FullInformation)
			globalDisplacement := make([]float64, len(degreeLocal))
			// if not found in global displacement, then it is a pinned
			// in local stiffiner matrix - than row and column is zero
			// for avoid collisian - we put a zero
			for i := 0; i < len(globalDisplacement); i++ {
				for j := 0; j < len(degreeGlobal); j++ {
					if degreeLocal[i] == degreeGlobal[j] {
						globalDisplacement[i] = x.Get(j, 0)
						break
					}
				}
			}

			t := linAlg.NewMatrix64bySize(10, 10)
			fe.GetCoordinateTransformation(&t)

			// Zo = T_t * Z
			var localDisplacement []float64
			for i := 0; i < t.GetRowSize(); i++ {
				sum := 0.0
				for j := 0; j < t.GetColumnSize(); j++ {
					sum += t.Get(i, j) * globalDisplacement[j]
				}
				localDisplacement = append(localDisplacement, sum)
			}

			kk := linAlg.NewMatrix64bySize(10, 10)
			fe.GetStiffinerK(&kk)

			var localForce []float64
			for i := 0; i < kk.GetRowSize(); i++ {
				sum := 0.0
				for j := 0; j < kk.GetRowSize(); j++ {
					sum += kk.Get(i, j) * localDisplacement[j]
				}
				localForce = append(localForce, sum)
			}

			//fmt.Println("local Force = ", localForce)
			if localForce[0] > 0.0 && localForce[3] < 0.0 {
				// TODO : it is not correct , because uniform load can change
				//fmt.Println("Compress")
			} else {
				// TODO: testing
				localForce[0] = 0.0
			}

			grLocal := linAlg.NewMatrix64bySize(6, 6)
			fe.GetPotentialGr(&grLocal, localForce[0])

			// Add local stiffiner matrix to global matrix
			for i := 0; i < len(degreeLocal); i++ {
				g, err := mapIndex.GetByAxe(degreeLocal[i])
				if err != nil {
					continue
				}
				for j := 0; j < len(degreeLocal); j++ {
					h, err := mapIndex.GetByAxe(degreeLocal[j])
					if err != nil {
						continue
					}
					potentialGlobal.Set(g, h, potentialGlobal.Get(g, h)+grLocal.Get(i, j))
				}
			}
		}

		//fmt.Println("PotentialGlobal = ", potentialGlobal)
		HoPotential := linAlg.NewMatrix64bySize(n, n)
		bufferPotential := linAlg.NewMatrix64bySize(n, 1)
		//fmt.Printf("lu = %#v\n", lu)
		for i := 0; i < n; i++ {
			// Create vertical vector from [Mo]
			for j := 0; j < n; j++ {
				bufferPotential.Set(j, 0, potentialGlobal.Get(j, i))
			}
			// Calculation
			result := lu.Solve(bufferPotential)
			// Add vector to [Ho]
			for j := 0; j < n; j++ {
				HoPotential.Set(j, i, result.Get(j, 0))
			}
		}
		//fmt.Println("[HoPotential] = ", HoPotential)
		{
			// TODO: check
			// Remove zero rows and columns
			var removePosition []int
			// TODO: len --> to matrix lenght
			// TODO: at the first check diagonal element
			for i := 0; i < HoPotential.GetRowSize(); i++ {
				found := false
				for j := 0; j < HoPotential.GetRowSize(); j++ {
					if HoPotential.Get(i, j) != 0.0 {
						found = true
						break
					}
				}
				if found {
					continue
				}
				removePosition = append(removePosition, i)
			}
			HoPotential.RemoveRowAndColumn(removePosition...)
		}
		// Calculation of
		eigenPotential := solver.NewEigen(HoPotential)
		//fmt.Println("lambda       = ", eigenPotential.GetRealEigenvalues())
		//fmt.Println("lambda Re    = ", eigenPotential.GetImagEigenvalues())
		//fmt.Println("eigenvectors = ", eigenPotential.GetV())
		//fmt.Println("getD = ", eigenPotential.GetD())

		// TODO: Remove strange results
		valueP := eigenPotential.GetRealEigenvalues()
		fmt.Println("Linear buckling loads:")
		for _, v := range valueP {
			fmt.Printf("P = %.5v\n", 1.0/v)
		}

		///  BUckling iteration solving
		// [K]  = stiffinerKGlobal
		// [Kg] = potentialGlobal
		//	for iter := 0; iter < 1000; iter++ {

		//}

		// Nolinear buckling calculation
		// algorithm Newton-Raphfon
		loadsOld := zeroCopy(m.forceCases[caseNumber])
		displacementOld := zeroDisplacement(...)
		resultOld := converge
		displacementOld, resultOld = calculate(loadsOld)

		loadsNew := m.forceCases[caseNumber]
		var displacementNew Matrix64
		var resultNew  resultNolinearBuckling
		displacementNew, resultNew = calculate(loadsNew)


		eps := 0.01
		for {
			
			if deltaDisp(displacementNew, displacementOld) <= eps && deltaLoads(loadsNew, loadsOld) <= eps && resultOld == converge && resultNew == diverge {
				break
			}
			switch resultNew{
			case converge:
				loadsOld = loadsNew
				loadsNew = mulpiply(2.0, loadsNew)
			case diverge:
				loadsNew = average(loadsNew, loadsOld)// (loadsNew + loadsOld)/2.0
			}
			displacementNew, resultNew = calculate(loadsNew)
		}
	}
	return nil
}

type resultNolinearBuckling int
const(
	diverge resultNolinearBuckling = iota
	converge resultNolinearBuckling
)


func (m *Dim2) getBeamFiniteElement(inx element.BeamIndex) (fe finiteElement.FiniteElementer) {
	material, err := m.getMaterial(inx)
	if err != nil {
		panic(fmt.Errorf("Cannot found material for beam #%v. Error = %v", inx, err))
	}
	shape, err := m.getShape(inx)
	if err != nil {
		panic(fmt.Errorf("Cannot found shape for beam #%v. Error = %v", inx, err))
	}
	coord, err := m.getCoordinate(inx)
	if err != nil {
		panic(fmt.Errorf("Cannot calculate lenght for beam #%v. Error = %v", inx, err))
	}
	if m.isTruss(inx) {
		f := finiteElement.TrussDim2{
			Material: material,
			Shape:    shape,
			Points:   coord,
		}
		return &f
	} /* else {
		fe := finiteElement.BeamDim2{
			Material: material,
			Shape:    shape,
			Points:   coord,
		}
		err = fe.GetStiffinerK(&buffer)
		if err != nil {
			return err
		}
	}*/
	return nil
}

func (m *Dim2) convertFromLocalToGlobalSystem(degreeGlobal *[]dof.AxeNumber, dofSystem *dof.DoF, mapIndex *dof.MapIndex, f func(finiteElement.FiniteElementer, *dof.DoF, finiteElement.Information) (linAlg.Matrix64, []dof.AxeNumber)) linAlg.Matrix64 {
	globalResult := linAlg.NewMatrix64bySize(len(*degreeGlobal), len(*degreeGlobal))
	for _, beam := range m.beams {
		fe := m.getBeamFiniteElement(beam.Index)
		klocal, degreeLocal := f(fe, dofSystem, finiteElement.WithoutZeroStiffiner)
		// Add local stiffiner matrix to global matrix
		for i := 0; i < len(degreeLocal); i++ {
			g, err := mapIndex.GetByAxe(degreeLocal[i])
			if err != nil {
				continue
			}
			for j := 0; j < len(degreeLocal); j++ {
				h, err := mapIndex.GetByAxe(degreeLocal[j])
				if err != nil {
					continue
				}
				globalResult.Set(g, h, globalResult.Get(g, h)+klocal.Get(i, j))
			}
		}
	}
	return globalResult
}
