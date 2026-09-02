-- Long-format ch-xbrl facts (all sample members) → wide rows using column_map.csv.
-- Caller creates tables extract_all, col_map.
-- ch-xbrl emits company_number; alias to company_id so extract_wide matches the
-- stream-read-xbrl oracle (which we do not change).

CREATE OR REPLACE MACRO is_plain_dim(d) AS (
    d IS NULL OR trim(cast(d AS VARCHAR)) IN ('', '{}')
);

CREATE OR REPLACE TABLE facts_src AS
SELECT
    fact_ord,
    trim(cast(company_number AS VARCHAR)) AS company_id,
    try_cast(nullif(trim(cast(period_start AS VARCHAR)), '') AS DATE) AS period_start,
    try_cast(nullif(trim(cast(period_end AS VARCHAR)), '') AS DATE) AS period_end,
    trim(cast(concept AS VARCHAR)) AS concept,
    nullif(trim(cast(value AS VARCHAR)), '') AS value,
    trim(coalesce(cast(dimensions AS VARCHAR), '')) AS dimensions,
    list_last(string_split(replace(cast(source_file AS VARCHAR), '\', '/'), '/')) AS source_file
FROM extract_all;

CREATE OR REPLACE TABLE facts_mapped AS
SELECT
    f.fact_ord,
    f.source_file,
    f.company_id,
    f.period_start,
    f.period_end,
    m.column_name,
    m.priority,
    m.grain,
    m.transform,
    CASE
        WHEN m.transform = 'not_bool' AND lower(f.value) = 'true' THEN 'false'
        WHEN m.transform = 'not_bool' AND lower(f.value) = 'false' THEN 'true'
        WHEN m.transform = 'abs' AND try_cast(replace(f.value, ',', '') AS DOUBLE) IS NOT NULL
            THEN cast(abs(try_cast(replace(f.value, ',', '') AS DOUBLE)) AS VARCHAR)
        ELSE f.value
    END AS value,
    f.dimensions
FROM facts_src f
INNER JOIN col_map m ON f.concept = m.concept
WHERE f.value IS NOT NULL
  AND (m.grain = 'general' OR is_plain_dim(f.dimensions));

CREATE OR REPLACE TABLE facts_best AS
SELECT * EXCLUDE (rn)
FROM (
    SELECT
        *,
        row_number() OVER (
            PARTITION BY
                source_file,
                company_id,
                column_name,
                period_start,
                period_end
            ORDER BY
                priority ASC,
                fact_ord ASC
        ) AS rn
    FROM facts_mapped
    WHERE grain = 'periodical'
) t
WHERE rn = 1;

CREATE OR REPLACE TABLE facts_best_general AS
SELECT * EXCLUDE (rn)
FROM (
    SELECT
        *,
        row_number() OVER (
            PARTITION BY source_file, company_id, column_name
            ORDER BY priority ASC, fact_ord ASC
        ) AS rn
    FROM facts_mapped
    WHERE grain = 'general'
) t
WHERE rn = 1;

CREATE OR REPLACE TABLE wide_general AS
SELECT
    source_file,
    company_id,
    max(CASE WHEN column_name = 'balance_sheet_date' THEN value END) AS balance_sheet_date,
    max(CASE WHEN column_name = 'companies_house_registered_number' THEN value END)
        AS companies_house_registered_number,
    max(CASE WHEN column_name = 'entity_current_legal_name' THEN value END)
        AS entity_current_legal_name,
    max(CASE WHEN column_name = 'company_dormant' THEN value END) AS company_dormant
FROM facts_best_general
GROUP BY source_file, company_id;

CREATE OR REPLACE TABLE wide_periodical AS
SELECT
    source_file,
    company_id,
    period_start,
    period_end,
    max(CASE WHEN column_name = 'average_number_employees_during_period' THEN value END)
        AS average_number_employees_during_period,
    max(CASE WHEN column_name = 'tangible_fixed_assets' THEN value END) AS tangible_fixed_assets,
    max(CASE WHEN column_name = 'debtors' THEN value END) AS debtors,
    max(CASE WHEN column_name = 'cash_bank_in_hand' THEN value END) AS cash_bank_in_hand,
    max(CASE WHEN column_name = 'current_assets' THEN value END) AS current_assets,
    max(CASE WHEN column_name = 'creditors_due_within_one_year' THEN value END)
        AS creditors_due_within_one_year,
    max(CASE WHEN column_name = 'creditors_due_after_one_year' THEN value END)
        AS creditors_due_after_one_year,
    max(CASE WHEN column_name = 'net_current_assets_liabilities' THEN value END)
        AS net_current_assets_liabilities,
    max(CASE WHEN column_name = 'total_assets_less_current_liabilities' THEN value END)
        AS total_assets_less_current_liabilities,
    max(CASE WHEN column_name = 'net_assets_liabilities_including_pension_asset_liability' THEN value END)
        AS net_assets_liabilities_including_pension_asset_liability,
    max(CASE WHEN column_name = 'called_up_share_capital' THEN value END)
        AS called_up_share_capital,
    max(CASE WHEN column_name = 'profit_loss_account_reserve' THEN value END)
        AS profit_loss_account_reserve,
    max(CASE WHEN column_name = 'shareholder_funds' THEN value END) AS shareholder_funds,
    max(CASE WHEN column_name = 'turnover_gross_operating_revenue' THEN value END)
        AS turnover_gross_operating_revenue,
    max(CASE WHEN column_name = 'other_operating_income' THEN value END)
        AS other_operating_income,
    max(CASE WHEN column_name = 'cost_sales' THEN value END) AS cost_sales,
    max(CASE WHEN column_name = 'gross_profit_loss' THEN value END) AS gross_profit_loss,
    max(CASE WHEN column_name = 'administrative_expenses' THEN value END)
        AS administrative_expenses,
    max(CASE WHEN column_name = 'raw_materials_consumables' THEN value END)
        AS raw_materials_consumables,
    max(CASE WHEN column_name = 'staff_costs' THEN value END) AS staff_costs,
    max(CASE WHEN column_name = 'depreciation_other_amounts_written_off_tangible_intangible_fixed_assets' THEN value END)
        AS depreciation_other_amounts_written_off_tangible_intangible_fixed_assets,
    max(CASE WHEN column_name = 'other_operating_charges_format2' THEN value END)
        AS other_operating_charges_format2,
    max(CASE WHEN column_name = 'operating_profit_loss' THEN value END)
        AS operating_profit_loss,
    max(CASE WHEN column_name = 'profit_loss_on_ordinary_activities_before_tax' THEN value END)
        AS profit_loss_on_ordinary_activities_before_tax,
    max(CASE WHEN column_name = 'tax_on_profit_or_loss_on_ordinary_activities' THEN value END)
        AS tax_on_profit_or_loss_on_ordinary_activities,
    max(CASE WHEN column_name = 'profit_loss_for_period' THEN value END)
        AS profit_loss_for_period
FROM facts_best
GROUP BY source_file, company_id, period_start, period_end;

CREATE OR REPLACE TABLE periods AS
SELECT DISTINCT source_file, company_id, period_start, period_end FROM facts_best
UNION
SELECT DISTINCT g.source_file, g.company_id, NULL::DATE, NULL::DATE
FROM facts_best_general g
WHERE NOT EXISTS (
    SELECT 1 FROM facts_best p
    WHERE p.source_file = g.source_file AND p.company_id = g.company_id
);

CREATE OR REPLACE TABLE extract_wide AS
SELECT
    per.source_file,
    per.company_id,
    g.balance_sheet_date,
    g.companies_house_registered_number,
    g.entity_current_legal_name,
    g.company_dormant,
    p.period_start,
    p.period_end,
    p.average_number_employees_during_period,
    p.tangible_fixed_assets,
    p.debtors,
    p.cash_bank_in_hand,
    p.current_assets,
    p.creditors_due_within_one_year,
    p.creditors_due_after_one_year,
    p.net_current_assets_liabilities,
    p.total_assets_less_current_liabilities,
    p.net_assets_liabilities_including_pension_asset_liability,
    p.called_up_share_capital,
    p.profit_loss_account_reserve,
    p.shareholder_funds,
    p.turnover_gross_operating_revenue,
    p.other_operating_income,
    p.cost_sales,
    p.gross_profit_loss,
    p.administrative_expenses,
    p.raw_materials_consumables,
    p.staff_costs,
    p.depreciation_other_amounts_written_off_tangible_intangible_fixed_assets,
    p.other_operating_charges_format2,
    p.operating_profit_loss,
    p.profit_loss_on_ordinary_activities_before_tax,
    p.tax_on_profit_or_loss_on_ordinary_activities,
    p.profit_loss_for_period
FROM periods per
LEFT JOIN wide_periodical p
    ON p.source_file = per.source_file
   AND p.company_id = per.company_id
   AND p.period_start IS NOT DISTINCT FROM per.period_start
   AND p.period_end IS NOT DISTINCT FROM per.period_end
LEFT JOIN wide_general g
    ON g.source_file = per.source_file
   AND g.company_id = per.company_id;
