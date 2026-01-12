import { SandboxResponse, RunCodeResponse, RunOptions, UploadFileResponse } from './types';


export class SandboxClient {
  private baseUrl: string;
  private apiKey: string;

  constructor(baseUrl: string, apiKey: string) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.apiKey = apiKey;
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const headers: Record<string, string> = {
      'X-API-KEY': this.apiKey,
      ...((options.headers as Record<string, string>) || {}),
    };

    const response = await fetch(url, {
      ...options,
      headers,
    });

    if (!response.ok) {
      let errorMsg = `Request failed with status ${response.status}: ${response.statusText}`;
      try {
        const json = (await response.json()) as SandboxResponse<T>;
        if (json.code !== 0) {
          let msg = json.message || 'Unknown error'; // Types.ts says it has message.
          errorMsg = `Sandbox API error (${json.code}): ${msg}`;
        } else if (json.message) {
          errorMsg = `Request failed with status ${response.status}: ${json.message}`;
        }
      } catch (e) {
        // If response is not JSON
      }
      throw new Error(errorMsg);
    }

    const json = (await response.json()) as SandboxResponse<T>;
    if (json.code !== 0) {
      let msg = json.message || 'Unknown error';
      throw new Error(`Sandbox API error (${json.code}): ${msg}`);
    }

    return json.data;
  }

  /**
   * Upload a file to the sandbox storage.
   * @param file Blob, File, Buffer or Stream. If running in Node.js and passing a Buffer, provide filename in options implicitly if possible, or use FormData construction manually?
   * Ideally we accept a FormData compatible object or create one.
   * For Node.js `fetch` with `FormData`:
   * Native fetch in Node 20+ supports FormData.
   */
  async uploadFile(file: File | Blob | any, filename?: string): Promise<{ file_id: string }> {
    const formData = new FormData();
    formData.append('file', file, filename); // 'file' is the field name expected by Gin

    const result = await this.request<UploadFileResponse>('/v1/sandbox/files', {
      method: 'POST',
      body: formData,
      // Header Content-Type is set automatically by fetch when body is FormData
    });
    return result;
  }

  /**
   * Download a file from the sandbox storage.
   * Returns a Blob. In Node.js environment this is a Blob.
   */
  async downloadFile(fileId: string): Promise<Blob> {
    const url = `${this.baseUrl}/v1/sandbox/files/${fileId}`;
    const response = await fetch(url, {
      method: 'GET',
      headers: {
        'X-API-KEY': this.apiKey,
      },
    });

    if (!response.ok) {
      throw new Error(`Download failed with status ${response.status}`);
    }
    return await response.blob();
  }

  // Helper to get text from download
  async downloadFileText(fileId: string): Promise<string> {
    const blob = await this.downloadFile(fileId);
    return await blob.text();
  }

  async runPython(code: string, options: RunOptions = {}): Promise<RunCodeResponse> {
    const body = {
      language: 'python3',
      code,
      preload: options.preload,
      enable_network: options.enable_network,
      files: options.input_files, // Map<filename, file_id>
      fetch_files: options.fetch_files,
    };

    return await this.request<RunCodeResponse>('/v1/sandbox/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  }

  async runNodeJs(code: string, options: RunOptions = {}): Promise<RunCodeResponse> {
    const body = {
      language: 'nodejs',
      code,
      preload: options.preload,
      enable_network: options.enable_network,
      files: options.input_files,
      fetch_files: options.fetch_files,
    };

    return await this.request<RunCodeResponse>('/v1/sandbox/run', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
  }
}
