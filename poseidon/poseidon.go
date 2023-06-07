package poseidon

import (
	"github.com/consensys/gnark/frontend"
	"github.com/succinctlabs/gnark-plonky2-verifier/field"
)

const HALF_N_FULL_ROUNDS = 4
const N_PARTIAL_ROUNDS = 22
const MAX_WIDTH = 12
const SPONGE_WIDTH = 12
const SPONGE_RATE = 8

type PoseidonState = [SPONGE_WIDTH]field.F
type PoseidonStateExtension = [SPONGE_WIDTH]field.QuadraticExtension
type PoseidonHashOut = [4]field.F

type PoseidonChip struct {
	api      frontend.API                 `gnark:"-"`
	fieldAPI field.FieldAPI               `gnark:"-"`
	qeAPI    *field.QuadraticExtensionAPI `gnark:"-"`
}

func NewPoseidonChip(api frontend.API, fieldAPI field.FieldAPI, qeAPI *field.QuadraticExtensionAPI) *PoseidonChip {
	return &PoseidonChip{api: api, fieldAPI: fieldAPI, qeAPI: qeAPI}
}

func (c *PoseidonChip) Poseidon(input PoseidonState) PoseidonState {
	state := input
	roundCounter := 0
	state = c.FullRounds(state, &roundCounter)
	state = c.PartialRounds(state, &roundCounter)
	state = c.FullRounds(state, &roundCounter)
	return state
}

func (c *PoseidonChip) HashNToMNoPad(input []field.F, nbOutputs int) []field.F {
	var state PoseidonState

	for i := 0; i < SPONGE_WIDTH; i++ {
		state[i] = field.ZERO_F
	}

	for i := 0; i < len(input); i += SPONGE_RATE {
		for j := 0; j < SPONGE_RATE; j++ {
			if i+j < len(input) {
				state[j] = input[i+j]
			}
		}
		state = c.Poseidon(state)
	}

	var outputs []field.F

	for {
		for i := 0; i < SPONGE_RATE; i++ {
			outputs = append(outputs, state[i])
			if len(outputs) == nbOutputs {
				return outputs
			}
		}
		state = c.Poseidon(state)
	}
}

func (c *PoseidonChip) HashNoPad(input []field.F) PoseidonHashOut {
	var hash PoseidonHashOut
	copy(hash[:], c.HashNToMNoPad(input, 4))
	return hash
}

func (c *PoseidonChip) ToVec(hash PoseidonHashOut) []field.F {
	return hash[:]
}

func (c *PoseidonChip) FullRounds(state PoseidonState, roundCounter *int) PoseidonState {
	for i := 0; i < HALF_N_FULL_ROUNDS; i++ {
		state = c.ConstantLayer(state, roundCounter)
		state = c.SBoxLayer(state)
		state = c.MdsLayer(state)
		*roundCounter += 1
	}
	return state
}

func (c *PoseidonChip) PartialRounds(state PoseidonState, roundCounter *int) PoseidonState {
	state = c.PartialFirstConstantLayer(state)
	state = c.MdsPartialLayerInit(state)

	for i := 0; i < N_PARTIAL_ROUNDS; i++ {
		state[0] = c.SBoxMonomial(state[0])
		state[0] = c.fieldAPI.Add(state[0], FAST_PARTIAL_ROUND_CONSTANTS[i])
		state = c.MdsPartialLayerFast(state, i)
	}

	*roundCounter += N_PARTIAL_ROUNDS

	return state
}

func (c *PoseidonChip) ConstantLayer(state PoseidonState, roundCounter *int) PoseidonState {
	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			roundConstant := ALL_ROUND_CONSTANTS[i+SPONGE_WIDTH*(*roundCounter)]
			state[i] = c.fieldAPI.Add(state[i], roundConstant)
		}
	}
	return state
}

func (c *PoseidonChip) ConstantLayerExtension(state PoseidonStateExtension, roundCounter *int) PoseidonStateExtension {
	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			roundConstant := c.qeAPI.FieldToQE(ALL_ROUND_CONSTANTS[i+SPONGE_WIDTH*(*roundCounter)])
			state[i] = c.qeAPI.AddExtension(state[i], roundConstant)
		}
	}
	return state
}

func (c *PoseidonChip) SBoxMonomial(x field.F) field.F {
	x2 := c.fieldAPI.Mul(x, x)
	x4 := c.fieldAPI.Mul(x2, x2)
	x3 := c.fieldAPI.Mul(x, x2)
	return c.fieldAPI.Mul(x3, x4)
}

func (c *PoseidonChip) SBoxMonomialExtension(x field.QuadraticExtension) field.QuadraticExtension {
	x2 := c.qeAPI.SquareExtension(x)
	x4 := c.qeAPI.SquareExtension(x2)
	x3 := c.qeAPI.MulExtension(x, x2)
	return c.qeAPI.MulExtension(x3, x4)
}

func (c *PoseidonChip) SBoxLayer(state PoseidonState) PoseidonState {
	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			state[i] = c.SBoxMonomial(state[i])
		}
	}
	return state
}

func (c *PoseidonChip) SBoxLayerExtension(state PoseidonStateExtension) PoseidonStateExtension {
	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			state[i] = c.SBoxMonomialExtension(state[i])
		}
	}
	return state
}

func (c *PoseidonChip) MdsRowShf(r int, v [SPONGE_WIDTH]frontend.Variable) frontend.Variable {
	res := ZERO_VAR

	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			res1 := c.api.Mul(v[(i+r)%SPONGE_WIDTH], MDS_MATRIX_CIRC_VARS[i])
			res = c.api.Add(res, res1)
		}
	}

	res = c.api.Add(res, c.api.Mul(v[r], MDS_MATRIX_DIAG_VARS[r]))
	return res
}

func (c *PoseidonChip) MdsRowShfExtension(r int, v [SPONGE_WIDTH]field.QuadraticExtension) field.QuadraticExtension {
	res := c.qeAPI.FieldToQE(field.ZERO_F)

	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			matrixVal := c.qeAPI.FieldToQE(MDS_MATRIX_CIRC[i])
			res1 := c.qeAPI.MulExtension(v[(i+r)%SPONGE_WIDTH], matrixVal)
			res = c.qeAPI.AddExtension(res, res1)
		}
	}

	matrixVal := c.qeAPI.FieldToQE(MDS_MATRIX_DIAG[r])
	res = c.qeAPI.AddExtension(res, c.qeAPI.MulExtension(v[r], matrixVal))
	return res
}

func (c *PoseidonChip) MdsLayer(state_ PoseidonState) PoseidonState {
	var result PoseidonState
	for i := 0; i < SPONGE_WIDTH; i++ {
		result[i] = field.ZERO_F
	}

	var state [SPONGE_WIDTH]frontend.Variable
	for i := 0; i < SPONGE_WIDTH; i++ {
		reducedState := c.fieldAPI.Reduce(state_[i])
		//state[i] = c.api.FromBinary(c.fieldAPI.ToBits(reducedState)...)
		state[i] = reducedState.Limbs[0]
	}

	for r := 0; r < 12; r++ {
		if r < SPONGE_WIDTH {
			sum := c.MdsRowShf(r, state)
			bits := c.api.ToBinary(sum)
			result[r] = c.fieldAPI.FromBits(bits...)
		}
	}

	return result
}

func (c *PoseidonChip) MdsLayerExtension(state_ PoseidonStateExtension) PoseidonStateExtension {
	var result PoseidonStateExtension

	for r := 0; r < 12; r++ {
		if r < SPONGE_WIDTH {
			sum := c.MdsRowShfExtension(r, state_)
			result[r] = sum
		}
	}

	return result
}

func (c *PoseidonChip) PartialFirstConstantLayer(state PoseidonState) PoseidonState {
	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			state[i] = c.fieldAPI.Add(state[i], FAST_PARTIAL_FIRST_ROUND_CONSTANT[i])
		}
	}
	return state
}

func (c *PoseidonChip) PartialFirstConstantLayerExtension(state PoseidonStateExtension) PoseidonStateExtension {
	for i := 0; i < 12; i++ {
		if i < SPONGE_WIDTH {
			state[i] = c.qeAPI.AddExtension(state[i], c.qeAPI.FieldToQE(FAST_PARTIAL_FIRST_ROUND_CONSTANT[i]))
		}
	}
	return state
}

func (c *PoseidonChip) MdsPartialLayerInit(state PoseidonState) PoseidonState {
	var result PoseidonState
	for i := 0; i < 12; i++ {
		result[i] = field.ZERO_F
	}

	result[0] = state[0]

	for r := 1; r < 12; r++ {
		if r < SPONGE_WIDTH {
			for d := 1; d < 12; d++ {
				if d < SPONGE_WIDTH {
					t := FAST_PARTIAL_ROUND_INITIAL_MATRIX[r-1][d-1]
					result[d] = c.fieldAPI.Add(result[d], c.fieldAPI.Mul(state[r], t))
				}
			}
		}
	}

	return result
}

func (c *PoseidonChip) MdsPartialLayerInitExtension(state PoseidonStateExtension) PoseidonStateExtension {
	var result PoseidonStateExtension
	for i := 0; i < 12; i++ {
		result[i] = c.qeAPI.FieldToQE(field.ZERO_F)
	}

	result[0] = state[0]

	for r := 1; r < 12; r++ {
		if r < SPONGE_WIDTH {
			for d := 1; d < 12; d++ {
				if d < SPONGE_WIDTH {
					t := c.qeAPI.FieldToQE(FAST_PARTIAL_ROUND_INITIAL_MATRIX[r-1][d-1])
					result[d] = c.qeAPI.AddExtension(result[d], c.qeAPI.MulExtension(state[r], t))
				}
			}
		}
	}

	return result
}

func (c *PoseidonChip) MdsPartialLayerFast(state PoseidonState, r int) PoseidonState {
	dSum := ZERO_VAR
	for i := 1; i < 12; i++ {
		if i < SPONGE_WIDTH {
			t := FAST_PARTIAL_ROUND_W_HATS_VARS[r][i-1]
			reducedState := c.fieldAPI.Reduce(state[i])
			//si := c.api.FromBinary(c.fieldAPI.ToBits(reducedState)...)
			si := reducedState.Limbs[0]
			dSum = c.api.Add(dSum, c.api.Mul(si, t))
		}
	}

	reducedState := c.fieldAPI.Reduce(state[0])
	//s0 := c.api.FromBinary(c.fieldAPI.ToBits(reducedState)...)
	s0 := reducedState.Limbs[0]
	dSum = c.api.Add(dSum, c.api.Mul(s0, MDS0TO0_VAR))
	d := c.fieldAPI.FromBits(c.api.ToBinary(dSum)...)
	//d := c.fieldAPI.NewElement(dSum)

	var result PoseidonState
	for i := 0; i < SPONGE_WIDTH; i++ {
		result[i] = field.ZERO_F
	}

	result[0] = d

	for i := 1; i < 12; i++ {
		if i < SPONGE_WIDTH {
			t := FAST_PARTIAL_ROUND_VS[r][i-1]
			result[i] = c.fieldAPI.Add(state[i], c.fieldAPI.Mul(state[0], t))
		}
	}

	return result
}

func (c *PoseidonChip) MdsPartialLayerFastExtension(state PoseidonStateExtension, r int) PoseidonStateExtension {
	s0 := state[0]
	mds0to0 := c.qeAPI.FieldToQE(MDS0TO0)
	d := c.qeAPI.MulExtension(s0, mds0to0)
	for i := 1; i < 12; i++ {
		if i < SPONGE_WIDTH {
			t := c.qeAPI.FieldToQE(FAST_PARTIAL_ROUND_W_HATS[r][i-1])
			d = c.qeAPI.AddExtension(d, c.qeAPI.MulExtension(state[i], t))
		}
	}

	var result PoseidonStateExtension
	result[0] = d
	for i := 1; i < 12; i++ {
		if i < SPONGE_WIDTH {
			t := c.qeAPI.FieldToQE(FAST_PARTIAL_ROUND_VS[r][i-1])
			result[i] = c.qeAPI.AddExtension(c.qeAPI.MulExtension(state[0], t), state[i])
		}
	}

	return result
}
