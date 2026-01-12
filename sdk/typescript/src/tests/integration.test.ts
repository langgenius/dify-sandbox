import { SandboxClient } from '../client';
import fs from 'fs';
import path from 'path';

// Helper to create a Blob from string (Node 18+)
function createBlob(content: string): Blob {
  return new Blob([content], { type: 'text/plain' });
}

async function runTest() {
  const baseUrl = 'http://localhost:8194';
  const apiKey = 'dify-sandbox'; // Default key
  const client = new SandboxClient(baseUrl, apiKey);

  console.log('--- Starting SDK Integration Test ---');

  try {
    // 1. Upload File
    console.log('1. Uploading file...');
    const testContent = 'Hello from SDK Integration Test';
    const fileBlob = createBlob(testContent);
    // In Node FormData, if passing Blob, we might need filename
    // The client implementation uses `formData.append('file', file, filename)`
    const uploadResp = await client.uploadFile(fileBlob, 'sdk_test.txt');
    console.log('Upload response:', uploadResp);
    const fileId = uploadResp.file_id;

    // 2. Run Python Code reading the file
    console.log('2. Running Python code...');
    const pythonCode = `
import os
with open('sdk_test.txt', 'r') as f:
    content = f.read()
print(f"Read content: {content}")
with open('sdk_output.txt', 'w') as f:
    f.write(content + " - VERIFIED")
`;
    const runResp = await client.runPython(pythonCode, {
      input_files: { 'sdk_test.txt': fileId },
      fetch_files: ['sdk_output.txt']
    });
    console.log('Run response:', runResp);

    if (runResp.error) {
      throw new Error(`Python execution failed: ${runResp.error}`);
    }

    if (!runResp.stdout.includes('Read content: Hello from SDK Integration Test')) {
      throw new Error('Stdout verification failed');
    }

    const outputFileId = runResp.files['sdk_output.txt'];
    if (!outputFileId) {
      throw new Error('Output file not returned');
    }

    // 3. Download Output File
    console.log('3. Downloading output file...');
    const downloadedText = await client.downloadFileText(outputFileId);
    console.log('Downloaded content:', downloadedText);

    if (downloadedText !== 'Hello from SDK Integration Test - VERIFIED') {
      throw new Error('Downloaded content verification failed');
    }

    console.log('--- Test Passed ---');
  } catch (error) {
    console.error('--- Test Failed ---');
    console.error(error);
    process.exit(1);
  }
}

runTest();
