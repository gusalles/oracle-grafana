import { DataQuery, DataSourceJsonData } from '@grafana/schema';

export interface MyQuery extends DataQuery {
  o_sql?: string;
  o_parsed?: string;
};

/**
 * These are options configured for each DataSource instance
 */
export interface MyDataSourceOptions extends DataSourceJsonData {
  o_connStr?: string;
  o_hostname?: string;
  o_port?: number;
  o_service?: string;
  o_sid?: string;
  o_tlsEnabled?: boolean;
  o_user?: string;

  // Wallet settings
  o_walletMode?: boolean;
  o_walletTnsAlias?: string;
  o_walletUploaded?: boolean;
};

/**
 * Value that is used in the backend, but never sent over HTTP to the frontend
 */
export interface MySecureJsonData {
  o_password?: string;
  o_walletPassword?: string;
  o_walletZip?: string;
};
