import tensorflow as tf
from onnx_tf.backend import prepare
import onnx

# Step 2: Save it as a `SavedModel`
saved_model_dir = "saved_model"

# Step 3: Convert SavedModel to ONNX
onnx_model_path = "model.onnx"

# Load the TensorFlow model and convert to ONNX
onnx_model = prepare(tf.saved_model.load(saved_model_dir)).export_model()

# Save the ONNX model
onnx.save(onnx_model, onnx_model_path)

print(f"Model successfully converted and saved as {onnx_model_path}")
