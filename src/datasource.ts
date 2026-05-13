import { DataSourceInstanceSettings, CoreApp, MetricFindValue, dateTime, DataFrame, DataQueryRequest, DataQueryResponse } from '@grafana/data';
import { DataSourceWithBackend } from '@grafana/runtime';
import { Observable, lastValueFrom, map, switchMap, catchError } from 'rxjs';

import { interpolate } from './interpolate';
import { MyQuery, MyDataSourceOptions } from './types';

// Cache for metricFindQuery results to prevent duplicate calls
const metricFindCache = new Map<string, { data: MetricFindValue[], timestamp: number }>();
const CACHE_TTL = 1000; // 1 second cache

export class DataSource extends DataSourceWithBackend<MyQuery, MyDataSourceOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<MyDataSourceOptions>) {
    super(instanceSettings);
  }

  query(request: DataQueryRequest<MyQuery>): Observable<DataQueryResponse> {
    for(const query of request.targets) {
      // Always interpolate, even if scopedVars is empty
      // This ensures variables are resolved from the template service
      query.o_parsed = interpolate(query.o_sql || '', request.scopedVars);
      query.o_sql = interpolate(query.o_sql || '');
    }
    return super.query(request)
  }

  /**
   * Method implemented to use the Query variable available to this datasource.
   * @param query User defined query.
   * @param options Query options.
   * @returns 
   */
  async metricFindQuery(query: string, options?: any): Promise<MetricFindValue[]> {
    if (!query) {
      return Promise.resolve([]);
    }

    const scopedVars = options?.scopedVars ?? {};
    
    // Create a cache key based on query and scopedVars
    const cacheKey = JSON.stringify({ query, scopedVars });
    const now = Date.now();
    
    // Check cache first
    const cached = metricFindCache.get(cacheKey);
    if (cached && (now - cached.timestamp) < CACHE_TTL) {
      return cached.data;
    }
    
    // For the first variable in a cascade, scopedVars might be empty
    // We need to handle this case to avoid cancellation
    let parsedQuery = query;
    
    // Only interpolate if the query actually contains variables
    // This prevents unnecessary processing for simple queries
    if (query.includes('${')) {
      parsedQuery = interpolate(query, scopedVars);
    }

    // Create a unique request ID to prevent Grafana from cancelling it
    const uniqueRequestId = `metricFindQuery-${Date.now()}-${Math.random()}`;
    
    try {
      const response = this.query({
        interval: '',
        intervalMs: 0,
        requestId: uniqueRequestId,
        range: {
          from: dateTime(),
          to: dateTime(),
          raw: {
            from: dateTime(),
            to: dateTime()
          }
        },
        scopedVars,
        targets: [{
          datasource: this.getDefaultQuery(CoreApp.Unknown).datasource,
          o_sql: query,
          o_parsed: parsedQuery,
          refId: 'A'
        }],
        timezone: 'Z',
        app: '',
        startTime: 0,
      });

      // Add proper error handling to prevent cancellation from breaking the variable
      const result = await lastValueFrom(response.pipe(
        switchMap(response => {
          if (!response.data || response.data.length === 0) {
            return [];
          }
          return response.data;
        }),
        switchMap((data: DataFrame) => {
          if (!data.fields || data.fields.length === 0) {
            return [];
          }
          return data.fields;
        }),
        map(field =>
          field.values.toArray().map(value => {
            return { text: value };
          })
        ),
        catchError(err => {
          console.error('MetricFindQuery error for query:', query, 'Error:', err);
          // Return empty array on error to prevent variable from breaking
          return [];
        })
      ));
      
      // Cache the result
      metricFindCache.set(cacheKey, { data: result, timestamp: now });
      
      return result;
    } catch (err) {
      console.error('MetricFindQuery caught error for query:', query, 'Error:', err);
      return [];
    }
  }

  getDefaultQuery(_: CoreApp): Partial<MyQuery> {
    return {
      o_sql: 'SELECT * \n FROM SYS.races \nWHERE data BETWEEN $__from AND $__to',
    }
  }
}
