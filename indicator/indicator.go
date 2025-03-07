package indicator

import (
	_ "embed"
	"math"

	"github.com/advancedclimatesystems/gonnx"
	"gorgonia.org/tensor"
	"trader.fun/pumpfun"
)

//go:embed indicator.onnx
var indicatorOnnx []byte

var (
	model = loadModel()
)

func ShouldBuy(coin *pumpfun.Coin) bool {
	compiledInputs := convertFloat64ToFloat32(coin.Compile())

	var inputs = make(gonnx.Tensors)
	inputs["inputs"] = tensor.New(
		tensor.WithShape(1, 15, 3, 1), // double check
		tensor.WithBacking(compiledInputs),
	)

	results, err := model.Run(inputs)
	if err != nil {
		panic(err)
	}

	outputTensor := results["output_0"]
	return math.Round(float64(outputTensor.Data().([]float32)[0])) == 1
}

func loadModel() *gonnx.Model {
	if len(indicatorOnnx) == 0 {
		return nil
	}
	model, err := gonnx.NewModelFromBytes(indicatorOnnx)
	if err != nil {
		panic(err)
	}
	return model
}

func convertFloat64ToFloat32(input []float64) []float32 {
	output := make([]float32, len(input))
	for i, v := range input {
		output[i] = float32(v)
	}
	return output
}
