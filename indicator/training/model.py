import tensorflow as tf
import numpy as np
from tensorflow.keras import layers, models
from tensorflow.keras.callbacks import EarlyStopping, ReduceLROnPlateau
from tensorflow.keras import regularizers
from sklearn.model_selection import train_test_split

def create_model(input_shape):
    model = models.Sequential()

    # First Convolutional Layer (minimal filters, small kernel)
    model.add(layers.Conv2D(1, kernel_size=(2, 2), activation='relu', input_shape=input_shape, kernel_regularizer=regularizers.l2(1e-4)))

    # Batch Normalization to improve convergence
    model.add(layers.BatchNormalization())

    # Flatten the output from CNN layers
    model.add(layers.Flatten())

    # Dense Layer for feature extraction with regularization
    model.add(layers.Dense(4, activation='relu', kernel_regularizer=regularizers.l2(1e-4)))

    # Dropout for better generalization
    model.add(layers.Dropout(0.3))
    
    # Output Layer for binary classification (1 unit, sigmoid activation)
    model.add(layers.Dense(1, activation='sigmoid'))

    # Compile the model with binary cross-entropy loss
    model.compile(optimizer='adam', loss='binary_crossentropy', metrics=['accuracy'])

    return model

def load_dataset(file_path):
    inputs = []
    outputs = []
    
    with open(file_path, 'r') as file:
        for line in file:
            line = line.strip()  # Remove leading/trailing whitespace
            if not line:
                continue
            # Split the line by '=>'
            data, output = line.split('=>')
            
            # Parse the input values as floats, split by commas
            input_values = np.array([float(x) for x in data.split(',')], dtype=np.float32)
            inputs.append(input_values)
            
            # Parse the output value as a float
            outputs.append(float(output))
    
    # Convert the lists to numpy arrays for model training
    return np.array(inputs), np.array(outputs)

# Example usage:
inputs, outputs = load_dataset("dataset.txt")

print(f"Number of inputs loaded: {inputs.shape[0]}")
print(f"Number of outputs loaded: {outputs.shape[0]}")

# Assuming input shape is (15, 3, 1) (15x3 matrix, with 1 channel for CNN)
input_shape = (15, 3, 1)

# Create the CNN model
model = create_model(input_shape)
model.summary()

# Reshape inputs to match (15, 3, 1) to work with CNN layers
inputs_reshaped = inputs.reshape((-1, 15, 3, 1))

# EarlyStopping callback to stop training when validation loss stops improving
early_stopping = EarlyStopping(monitor='val_loss', patience=15, restore_best_weights=True)

# ReduceLROnPlateau to reduce learning rate when validation loss plateaus
reduce_lr = ReduceLROnPlateau(monitor='val_loss', factor=0.5, patience=15, min_lr=1e-6)

# Split data into training and validation sets (80% training, 20% validation)
inputs_train, inputs_val, outputs_train, outputs_val = train_test_split(inputs_reshaped, outputs, test_size=0.2, random_state=42)

# Train the model with a smaller batch size to handle the small dataset
model.fit(
    inputs_train, 
    outputs_train, 
    validation_data=(inputs_val, outputs_val),
    epochs=100,  # Lower epochs for small dataset
    batch_size=2,  # Small batch size for smaller updates
    callbacks=[early_stopping, reduce_lr]
)

# Save the model
tf.saved_model.save(model, "saved_model")

print("Model saved as saved_model py -m tf2onnx.convert --saved-model saved_model --output model.onnx")

# Evaluate the model on the validation data
val_loss, val_accuracy = model.evaluate(inputs_val, outputs_val)
print(f"Validation Loss: {val_loss}, Validation Accuracy: {val_accuracy}")
