import React, { ChangeEvent, useEffect, useRef, useState } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Alert, Button, InlineField, InlineSwitch, Input, Legend, SecretInput, Select, Stack } from '@grafana/ui';
import { MyDataSourceOptions, MySecureJsonData } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<MyDataSourceOptions> { }

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const [walletAliases, setWalletAliases] = useState<string[]>([]);
  const [walletUploading, setWalletUploading] = useState(false);
  const [walletError, setWalletError] = useState<string | null>(null);
  const [walletSuccess, setWalletSuccess] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const walletMode = options.jsonData.o_walletMode ?? false;
  const dsUid = options.uid ?? '';

  // Fetch TNS aliases when wallet mode is active
  useEffect(() => {
    if (!walletMode || !dsUid) {
      return;
    }
    const sub = getBackendSrv()
      .fetch<{ tnsAliases: string[]; uploaded: boolean }>({
        url: `/api/datasources/uid/${dsUid}/resources/wallet-aliases`,
        method: 'GET',
      })
      .subscribe({
        next: (response) => {
          const data = response.data;
          if (data.tnsAliases) {
            setWalletAliases(data.tnsAliases);
          }
        },
        error: () => {
          setWalletAliases([]);
        },
      });
    return () => sub.unsubscribe();
  }, [walletMode, dsUid]);

  const onConnStrChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_connStr: event.target.value,
      }
    });
  };

  const onHostnameChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_hostname: event.target.value,
      }
    });
  };

  const onPortChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_port: Number(event.target.value),
      }
    });
  };

  const onServiceChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_service: event.target.value,
        o_sid: ''
      }
    });
  };

  const onSIDChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_service: '',
        o_sid: event.target.value,
      }
    });
  };

  const onTlsEnabledChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_tlsEnabled: event.target.checked,
      },
    });
  };

  const onUserChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_user: event.target.value,
      }
    });
  };

  // Secure field (only sent to the backend)
  const onPasswordChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...options.secureJsonData,
        o_password: event.target.value,
      },
    });
  };

  const onPasswordReset = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...options.secureJsonFields,
        o_password: false
      },
      secureJsonData: {
        ...options.secureJsonData,
        o_password: ''
      },
    });
  };

  // Wallet mode toggle
  const onWalletModeChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_walletMode: event.target.checked,
      },
    });
  };

  // Wallet password
  const onWalletPasswordChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: {
        ...options.secureJsonData,
        o_walletPassword: event.target.value,
      },
    });
  };

  const onWalletPasswordReset = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: {
        ...options.secureJsonFields,
        o_walletPassword: false,
      },
      secureJsonData: {
        ...options.secureJsonData,
        o_walletPassword: '',
      },
    });
  };

  // Wallet TNS alias selection
  const onTnsAliasChange = (value: { value?: string }) => {
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_walletTnsAlias: value.value ?? '',
      },
    });
  };

  // Wallet file upload
  const onWalletUpload = () => {
    fileInputRef.current?.click();
  };

  const onFileSelected = (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }

    if (file.size > 10 * 1024 * 1024) {
      setWalletError('Wallet file exceeds maximum size of 10 MB');
      return;
    }

    if (!file.name.toLowerCase().endsWith('.zip')) {
      setWalletError('Please upload a .zip file');
      return;
    }

    // Clear previous state before uploading new wallet
    setWalletError(null);
    setWalletSuccess(null);
    setWalletAliases([]);
    onOptionsChange({
      ...options,
      jsonData: {
        ...options.jsonData,
        o_walletTnsAlias: '',
      },
    });

    setWalletUploading(true);

    const reader = new FileReader();
    reader.onload = () => {
      const arrayBuffer = reader.result as ArrayBuffer;
      const bytes = new Uint8Array(arrayBuffer);
      let binary = '';
      for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
      }
      const base64Zip = btoa(binary);
      const blob = new Blob([arrayBuffer], { type: 'application/octet-stream' });

      getBackendSrv()
        .fetch<{ message: string; tnsAliases: string[] }>({
          url: `/api/datasources/uid/${options.uid}/resources/wallet-upload`,
          method: 'POST',
          headers: { 'Content-Type': 'application/octet-stream' },
          data: blob,
        })
        .subscribe({
          next: (response) => {
            const data = response.data;
            setWalletSuccess(data.message || 'Wallet uploaded successfully');
            setWalletUploading(false);
            if (data.tnsAliases) {
              setWalletAliases(data.tnsAliases);
            }
            onOptionsChange({
              ...options,
              jsonData: {
                ...options.jsonData,
                o_walletUploaded: true,
                o_walletTnsAlias: data.tnsAliases?.length === 1 ? data.tnsAliases[0] : options.jsonData.o_walletTnsAlias,
              },
              secureJsonData: {
                ...options.secureJsonData,
                o_walletZip: base64Zip,
              },
            });
          },
          error: (err) => {
            const errMsg = err?.data?.error || err?.message || 'Failed to upload wallet';
            setWalletError(errMsg);
            setWalletUploading(false);
          },
        });
    };
    reader.onerror = () => {
      setWalletError('Failed to read file');
      setWalletUploading(false);
    };
    reader.readAsArrayBuffer(file);

    // Reset file input so the same file can be re-uploaded
    event.target.value = '';
  };

  // Wallet delete
  const onWalletDelete = () => {
    setWalletError(null);
    setWalletSuccess(null);
    getBackendSrv()
      .fetch({
        url: `/api/datasources/uid/${options.uid}/resources/wallet`,
        method: 'DELETE',
      })
      .subscribe({
        next: () => {
          setWalletAliases([]);
          setWalletSuccess('Wallet deleted. Save & Test to finalize.');
          onOptionsChange({
            ...options,
            jsonData: {
              ...options.jsonData,
              o_walletUploaded: false,
              o_walletTnsAlias: '',
            },
            secureJsonData: {
              ...options.secureJsonData,
              o_walletZip: '',
            },
            secureJsonFields: {
              ...options.secureJsonFields,
              o_walletZip: false,
            },
          });
        },
        error: (err) => {
          setWalletError(err?.data?.error || 'Failed to delete wallet');
        },
      });
  };

  const { jsonData, secureJsonFields } = options;
  const secureJsonData = (options.secureJsonData || {}) as MySecureJsonData;

  const tnsAliasOptions = walletAliases.map((alias) => ({ label: alias, value: alias }));

  return (
    <Stack direction='column'>
      <Legend>
        Authentication
      </Legend>
      <Stack direction='row'>
        <InlineField grow label="User" labelWidth={12}>
          <Input
            placeholder={walletMode ? 'optional (falls back to wallet)' : 'oracle_user'}
            required={!walletMode}
            value={jsonData.o_user}
            width={40}
            onChange={onUserChange}
          />
        </InlineField>
        <InlineField grow label="Password" labelWidth={12}>
          <SecretInput
            isConfigured={(secureJsonFields && secureJsonFields.o_password) as boolean}
            placeholder={walletMode ? 'optional (falls back to wallet)' : 'oracle_password'}
            required={!walletMode}
            value={secureJsonData.o_password}
            width={40}
            onChange={onPasswordChange}
            onReset={onPasswordReset}
          />
        </InlineField>
      </Stack>

      <Legend>
        Connection Mode
      </Legend>
      <InlineField label="Use Oracle Wallet" labelWidth={20} tooltip="Enable to connect using an Oracle Cloud Wallet (ATP/ADW). When enabled, upload a wallet zip and select a TNS alias. Username/password fields above become optional and fall back to wallet-embedded credentials if left empty.">
        <InlineSwitch value={walletMode} onChange={onWalletModeChange} />
      </InlineField>

      {walletMode && (
        <>
          <Legend>
            Wallet Configuration
          </Legend>

          {walletError && <Alert title="Wallet Error" severity="error">{walletError}</Alert>}
          {walletSuccess && <Alert title="Wallet" severity="success">{walletSuccess}</Alert>}
          {jsonData.o_walletUploaded && (
            <Alert title="Before deleting this datasource" severity="info">
              If you plan to delete this datasource, click &ldquo;Delete Wallet&rdquo; first to remove the wallet files from Grafana storage.
            </Alert>
          )}

          <Stack direction='row'>
            <input
              type="file"
              accept=".zip"
              ref={fileInputRef}
              style={{ display: 'none' }}
              onChange={onFileSelected}
            />
            <Button
              variant="secondary"
              onClick={onWalletUpload}
              disabled={walletUploading}
              icon={walletUploading ? 'fa fa-spinner' : 'upload'}
              style={{ marginBottom: '1.5rem' }}
            >
              {walletUploading ? 'Uploading...' : jsonData.o_walletUploaded ? 'Re-upload Wallet Zip' : 'Upload Wallet Zip'}
            </Button>
            {jsonData.o_walletUploaded && (
              <Button variant="destructive" onClick={onWalletDelete} icon="trash-alt">
                Delete Wallet
              </Button>
            )}
          </Stack>

          <InlineField grow label="Wallet Password" labelWidth={20} tooltip="Password for the Oracle wallet (stored securely)">
            <SecretInput
              isConfigured={(secureJsonFields && secureJsonFields.o_walletPassword) as boolean}
              placeholder="wallet_password"
              value={secureJsonData.o_walletPassword}
              width={40}
              onChange={onWalletPasswordChange}
              onReset={onWalletPasswordReset}
            />
          </InlineField>

          <InlineField grow label="TNS Alias" labelWidth={20} tooltip="Select a TNS alias from the uploaded wallet's tnsnames.ora">
            <Select
              options={tnsAliasOptions}
              value={jsonData.o_walletTnsAlias}
              onChange={onTnsAliasChange}
              placeholder={walletAliases.length === 0 ? 'Upload a wallet first' : 'Select TNS alias'}
              disabled={walletAliases.length === 0}
              width={40}
              isClearable
            />
          </InlineField>
        </>
      )}

      {!walletMode && (
        <>
          <Legend>
            Connection
          </Legend>
          <InlineField grow label="ConnString" labelWidth={12}>
            <Input
              placeholder="(DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=localhost)(PORT=1521))(CONNECT_DATA=(SID=XE)))"
              value={jsonData.o_connStr}
              width={94}
              onChange={onConnStrChange}
            />
          </InlineField>
          <InlineField label="TLS/SSL" labelWidth={12} tooltip="Enable TLS/SSL encryption (TCPS). Required when connecting to Oracle listeners configured for secure transport. When using a ConnString with PROTOCOL=TCPS, TLS is enabled automatically.">
            <InlineSwitch value={jsonData.o_tlsEnabled ?? false} onChange={onTlsEnabledChange} />
          </InlineField>
          <Stack direction='row'>
            <InlineField grow label="Hostname" labelWidth={12}>
              <Input
                placeholder="localhost"
                value={jsonData.o_hostname}
                width={40}
                onChange={onHostnameChange}
              />
            </InlineField>
            <InlineField grow label="Port" labelWidth={12}>
              <Input
                placeholder="1521"
                type="number"
                value={jsonData.o_port}
                width={40}
                onChange={onPortChange}
              />
            </InlineField>
          </Stack>
          <Stack direction='row'>
            <InlineField grow label="Service" labelWidth={12}>
              <Input
                placeholder=""
                value={jsonData.o_service}
                width={40}
                onChange={onServiceChange}
              />
            </InlineField>
            <InlineField grow label="or SID" labelWidth={12}>
              <Input
                placeholder="XE"
                value={jsonData.o_sid}
                width={40}
                onChange={onSIDChange}
              />
            </InlineField>
          </Stack>
        </>
      )}
    </Stack>
  );
}
