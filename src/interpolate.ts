import { ScopedVars } from '@grafana/data';
import { getTemplateSrv } from '@grafana/runtime';

export function interpolate(o_sql: string, scopedVars?: ScopedVars): string {
  let o_parsed = o_sql;
  if (o_parsed.includes('$__from')) {
    o_parsed = o_parsed.replace(/\$__from/g, `TO_TIMESTAMP('1970-01-01 00:00:00', 'YYYY-MM-DD HH24:MI:SS') + NUMTODSINTERVAL($__from / 1000, 'SECOND')`);
  }
  if (o_parsed.includes('$__to')) {
    o_parsed = o_parsed.replace(/\$__to/g, `TO_TIMESTAMP('1970-01-01 00:00:00', 'YYYY-MM-DD HH24:MI:SS') + NUMTODSINTERVAL($__to / 1000, 'SECOND')`);
  }
  
  // Get the template service
  const templateSrv = getTemplateSrv();
  
  // For cascading variables, we need to ensure all variables are resolved
  // The issue occurs when variables depend on each other and $__all is selected
  let result = o_parsed;
  
  // First pass: Try with scopedVars if provided
  if (scopedVars && Object.keys(scopedVars).length > 0) {
    result = templateSrv.replace(o_parsed, scopedVars);
  } else {
    // When no scoped vars, use all dashboard variables
    result = templateSrv.replace(o_parsed);
  }
  
  // Second pass: If any variables remain unresolved, try without scoped vars
  // This ensures we get all dashboard variables including cascading ones
  if (result.includes('${')) {
    // Try again with the full variable context
    result = templateSrv.replace(o_parsed);
  }
  
  // Final check: If IN clauses are empty or contain __ALL__, it means variables weren't resolved properly
  // Replace empty IN clauses or __ALL__ clauses with a condition that will return all rows
  result = result.replace(/\s+IN\s*\(\s*(?:__ALL__\s*)*\)/g, ' IS NOT NULL');
  
  return result;
};
