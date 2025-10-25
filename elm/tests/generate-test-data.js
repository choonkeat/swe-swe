#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

// Compile the Elm module
console.log('Compiling Elm test data generator...');
try {
    execSync('elm make tests/GenerateTestData.elm --output=generate-test-data.js', {
        cwd: path.join(__dirname, '..'),
        stdio: 'inherit'
    });
} catch (error) {
    console.error('Failed to compile Elm module:', error);
    process.exit(1);
}

// Load the compiled JavaScript
const elmCode = fs.readFileSync(path.join(__dirname, '..', 'generate-test-data.js'), 'utf8');

// Create a minimal Elm runtime environment
const Elm = {};
eval(elmCode);

// Initialize the Elm app with output port
let outputData = null;
const app = Elm.GenerateTestData.init({
    flags: null
});

// Subscribe to the output port
if (app.ports && app.ports.output) {
    app.ports.output.subscribe(function(data) {
        outputData = data;
        
        // Parse and format the JSON
        const jsonData = JSON.parse(data);
        const formattedJson = JSON.stringify(jsonData, null, 2);
        
        // Create testdata directory if it doesn't exist
        const testDataDir = path.join(__dirname, '..', '..', 'cmd', 'swe-swe', 'testdata');
        if (!fs.existsSync(testDataDir)) {
            fs.mkdirSync(testDataDir, { recursive: true });
        }
        
        // Write the test data file
        const outputPath = path.join(testDataDir, 'elm_json_test_cases.json');
        fs.writeFileSync(outputPath, formattedJson);
        
        console.log(`Generated Elm test data: ${outputPath}`);
    });
} else {
    console.error('Output port not found in Elm app');
    process.exit(1);
}

// Clean up the temporary compiled file
fs.unlinkSync(path.join(__dirname, '..', 'generate-test-data.js'));