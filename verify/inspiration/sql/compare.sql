-- Oracle 38-col wide (table oracle_wide) vs extract pivot (extract_wide).
-- Soft match: whitespace, quotes, thousands separators, numeric equality, dates.

CREATE OR REPLACE MACRO soft_value(v) AS (
    replace(
        replace(
            regexp_replace(trim(coalesce(cast(v AS VARCHAR), '')), '\s+', ' ', 'g'),
            ',',
            ''
        ),
        '"',
        ''
    )
);

CREATE OR REPLACE MACRO soft_date(v) AS coalesce(
    try_strptime(soft_value(v), '%Y-%m-%d'),
    try_strptime(soft_value(v), '%d.%m.%y'),
    try_strptime(soft_value(v), '%d.%m.%Y'),
    try_strptime(soft_value(v), '%d/%m/%y'),
    try_strptime(soft_value(v), '%d/%m/%Y'),
    try_strptime(soft_value(v), '%d-%m-%Y'),
    try_strptime(soft_value(v), '%d %b %Y'),
    try_strptime(soft_value(v), '%d %B %Y')
);

CREATE OR REPLACE MACRO soft_bool(v) AS (
    CASE lower(soft_value(v))
        WHEN 'true' THEN 'true'
        WHEN 'false' THEN 'false'
        WHEN '1' THEN 'true'
        WHEN '0' THEN 'false'
        ELSE lower(soft_value(v))
    END
);

CREATE OR REPLACE MACRO soft_equal(a, b) AS (
    soft_value(a) = soft_value(b)
    OR (soft_value(a) = '' AND soft_value(b) = '')
    OR (
        try_cast(soft_value(a) AS DOUBLE) IS NOT NULL
        AND try_cast(soft_value(b) AS DOUBLE) IS NOT NULL
        AND try_cast(soft_value(a) AS DOUBLE) = try_cast(soft_value(b) AS DOUBLE)
    )
    OR (
        soft_date(a) IS NOT NULL
        AND soft_date(b) IS NOT NULL
        AND soft_date(a) = soft_date(b)
    )
    OR (soft_bool(a) IN ('true', 'false') AND soft_bool(a) = soft_bool(b))
);

CREATE OR REPLACE TABLE paired AS
SELECT
    o.period_start AS o_period_start,
    o.period_end AS o_period_end,
    e.period_start AS e_period_start,
    e.period_end AS e_period_end,
    (o.company_id IS NOT NULL OR o.period_start IS NOT NULL OR o.period_end IS NOT NULL) AS has_oracle,
    (e.company_id IS NOT NULL OR e.period_start IS NOT NULL OR e.period_end IS NOT NULL) AS has_extract,
    o.run_code AS o_run_code,
    e.run_code AS e_run_code,
    o.company_id AS o_company_id,
    e.company_id AS e_company_id,
    o.date AS o_date,
    e.date AS e_date,
    o.file_type AS o_file_type,
    e.file_type AS e_file_type,
    o.taxonomy AS o_taxonomy,
    e.taxonomy AS e_taxonomy,
    o.balance_sheet_date AS o_balance_sheet_date,
    e.balance_sheet_date AS e_balance_sheet_date,
    o.companies_house_registered_number AS o_companies_house_registered_number,
    e.companies_house_registered_number AS e_companies_house_registered_number,
    o.entity_current_legal_name AS o_entity_current_legal_name,
    e.entity_current_legal_name AS e_entity_current_legal_name,
    o.company_dormant AS o_company_dormant,
    e.company_dormant AS e_company_dormant,
    o.average_number_employees_during_period AS o_average_number_employees_during_period,
    e.average_number_employees_during_period AS e_average_number_employees_during_period,
    o.tangible_fixed_assets AS o_tangible_fixed_assets,
    e.tangible_fixed_assets AS e_tangible_fixed_assets,
    o.debtors AS o_debtors,
    e.debtors AS e_debtors,
    o.cash_bank_in_hand AS o_cash_bank_in_hand,
    e.cash_bank_in_hand AS e_cash_bank_in_hand,
    o.current_assets AS o_current_assets,
    e.current_assets AS e_current_assets,
    o.creditors_due_within_one_year AS o_creditors_due_within_one_year,
    e.creditors_due_within_one_year AS e_creditors_due_within_one_year,
    o.creditors_due_after_one_year AS o_creditors_due_after_one_year,
    e.creditors_due_after_one_year AS e_creditors_due_after_one_year,
    o.net_current_assets_liabilities AS o_net_current_assets_liabilities,
    e.net_current_assets_liabilities AS e_net_current_assets_liabilities,
    o.total_assets_less_current_liabilities AS o_total_assets_less_current_liabilities,
    e.total_assets_less_current_liabilities AS e_total_assets_less_current_liabilities,
    o.net_assets_liabilities_including_pension_asset_liability
        AS o_net_assets_liabilities_including_pension_asset_liability,
    e.net_assets_liabilities_including_pension_asset_liability
        AS e_net_assets_liabilities_including_pension_asset_liability,
    o.called_up_share_capital AS o_called_up_share_capital,
    e.called_up_share_capital AS e_called_up_share_capital,
    o.profit_loss_account_reserve AS o_profit_loss_account_reserve,
    e.profit_loss_account_reserve AS e_profit_loss_account_reserve,
    o.shareholder_funds AS o_shareholder_funds,
    e.shareholder_funds AS e_shareholder_funds,
    o.turnover_gross_operating_revenue AS o_turnover_gross_operating_revenue,
    e.turnover_gross_operating_revenue AS e_turnover_gross_operating_revenue,
    o.other_operating_income AS o_other_operating_income,
    e.other_operating_income AS e_other_operating_income,
    o.cost_sales AS o_cost_sales,
    e.cost_sales AS e_cost_sales,
    o.gross_profit_loss AS o_gross_profit_loss,
    e.gross_profit_loss AS e_gross_profit_loss,
    o.administrative_expenses AS o_administrative_expenses,
    e.administrative_expenses AS e_administrative_expenses,
    o.raw_materials_consumables AS o_raw_materials_consumables,
    e.raw_materials_consumables AS e_raw_materials_consumables,
    o.staff_costs AS o_staff_costs,
    e.staff_costs AS e_staff_costs,
    o.depreciation_other_amounts_written_off_tangible_intangible_fixed_assets
        AS o_depreciation_other_amounts_written_off_tangible_intangible_fixed_assets,
    e.depreciation_other_amounts_written_off_tangible_intangible_fixed_assets
        AS e_depreciation_other_amounts_written_off_tangible_intangible_fixed_assets,
    o.other_operating_charges_format2 AS o_other_operating_charges_format2,
    e.other_operating_charges_format2 AS e_other_operating_charges_format2,
    o.operating_profit_loss AS o_operating_profit_loss,
    e.operating_profit_loss AS e_operating_profit_loss,
    o.profit_loss_on_ordinary_activities_before_tax
        AS o_profit_loss_on_ordinary_activities_before_tax,
    e.profit_loss_on_ordinary_activities_before_tax
        AS e_profit_loss_on_ordinary_activities_before_tax,
    o.tax_on_profit_or_loss_on_ordinary_activities
        AS o_tax_on_profit_or_loss_on_ordinary_activities,
    e.tax_on_profit_or_loss_on_ordinary_activities
        AS e_tax_on_profit_or_loss_on_ordinary_activities,
    o.profit_loss_for_period AS o_profit_loss_for_period,
    e.profit_loss_for_period AS e_profit_loss_for_period,
    o.error AS o_error,
    e.error AS e_error
FROM oracle_wide o
FULL OUTER JOIN extract_wide e
    ON o.period_start IS NOT DISTINCT FROM e.period_start
   AND o.period_end IS NOT DISTINCT FROM e.period_end;

CREATE OR REPLACE TABLE cell_diffs AS
SELECT
    coalesce(cast(o_period_start AS VARCHAR), cast(e_period_start AS VARCHAR), '') AS period_start,
    coalesce(cast(o_period_end AS VARCHAR), cast(e_period_end AS VARCHAR), '') AS period_end,
    col,
    o_val,
    e_val,
    kind
FROM paired,
LATERAL (
    VALUES
        ('run_code', cast(o_run_code AS VARCHAR), cast(e_run_code AS VARCHAR), 'meta'),
        ('company_id', cast(o_company_id AS VARCHAR), cast(e_company_id AS VARCHAR), 'value'),
        ('date', cast(o_date AS VARCHAR), cast(e_date AS VARCHAR), 'meta'),
        ('file_type', cast(o_file_type AS VARCHAR), cast(e_file_type AS VARCHAR), 'meta'),
        ('taxonomy', cast(o_taxonomy AS VARCHAR), cast(e_taxonomy AS VARCHAR), 'meta'),
        ('balance_sheet_date', cast(o_balance_sheet_date AS VARCHAR), cast(e_balance_sheet_date AS VARCHAR), 'value'),
        ('companies_house_registered_number', cast(o_companies_house_registered_number AS VARCHAR), cast(e_companies_house_registered_number AS VARCHAR), 'value'),
        ('entity_current_legal_name', cast(o_entity_current_legal_name AS VARCHAR), cast(e_entity_current_legal_name AS VARCHAR), 'value'),
        ('company_dormant', cast(o_company_dormant AS VARCHAR), cast(e_company_dormant AS VARCHAR), 'value'),
        ('average_number_employees_during_period', cast(o_average_number_employees_during_period AS VARCHAR), cast(e_average_number_employees_during_period AS VARCHAR), 'value'),
        ('tangible_fixed_assets', cast(o_tangible_fixed_assets AS VARCHAR), cast(e_tangible_fixed_assets AS VARCHAR), 'value'),
        ('debtors', cast(o_debtors AS VARCHAR), cast(e_debtors AS VARCHAR), 'value'),
        ('cash_bank_in_hand', cast(o_cash_bank_in_hand AS VARCHAR), cast(e_cash_bank_in_hand AS VARCHAR), 'value'),
        ('current_assets', cast(o_current_assets AS VARCHAR), cast(e_current_assets AS VARCHAR), 'value'),
        ('creditors_due_within_one_year', cast(o_creditors_due_within_one_year AS VARCHAR), cast(e_creditors_due_within_one_year AS VARCHAR), 'value'),
        ('creditors_due_after_one_year', cast(o_creditors_due_after_one_year AS VARCHAR), cast(e_creditors_due_after_one_year AS VARCHAR), 'value'),
        ('net_current_assets_liabilities', cast(o_net_current_assets_liabilities AS VARCHAR), cast(e_net_current_assets_liabilities AS VARCHAR), 'value'),
        ('total_assets_less_current_liabilities', cast(o_total_assets_less_current_liabilities AS VARCHAR), cast(e_total_assets_less_current_liabilities AS VARCHAR), 'value'),
        ('net_assets_liabilities_including_pension_asset_liability', cast(o_net_assets_liabilities_including_pension_asset_liability AS VARCHAR), cast(e_net_assets_liabilities_including_pension_asset_liability AS VARCHAR), 'value'),
        ('called_up_share_capital', cast(o_called_up_share_capital AS VARCHAR), cast(e_called_up_share_capital AS VARCHAR), 'value'),
        ('profit_loss_account_reserve', cast(o_profit_loss_account_reserve AS VARCHAR), cast(e_profit_loss_account_reserve AS VARCHAR), 'value'),
        ('shareholder_funds', cast(o_shareholder_funds AS VARCHAR), cast(e_shareholder_funds AS VARCHAR), 'value'),
        ('turnover_gross_operating_revenue', cast(o_turnover_gross_operating_revenue AS VARCHAR), cast(e_turnover_gross_operating_revenue AS VARCHAR), 'value'),
        ('other_operating_income', cast(o_other_operating_income AS VARCHAR), cast(e_other_operating_income AS VARCHAR), 'value'),
        ('cost_sales', cast(o_cost_sales AS VARCHAR), cast(e_cost_sales AS VARCHAR), 'value'),
        ('gross_profit_loss', cast(o_gross_profit_loss AS VARCHAR), cast(e_gross_profit_loss AS VARCHAR), 'value'),
        ('administrative_expenses', cast(o_administrative_expenses AS VARCHAR), cast(e_administrative_expenses AS VARCHAR), 'value'),
        ('raw_materials_consumables', cast(o_raw_materials_consumables AS VARCHAR), cast(e_raw_materials_consumables AS VARCHAR), 'value'),
        ('staff_costs', cast(o_staff_costs AS VARCHAR), cast(e_staff_costs AS VARCHAR), 'value'),
        ('depreciation_other_amounts_written_off_tangible_intangible_fixed_assets', cast(o_depreciation_other_amounts_written_off_tangible_intangible_fixed_assets AS VARCHAR), cast(e_depreciation_other_amounts_written_off_tangible_intangible_fixed_assets AS VARCHAR), 'value'),
        ('other_operating_charges_format2', cast(o_other_operating_charges_format2 AS VARCHAR), cast(e_other_operating_charges_format2 AS VARCHAR), 'value'),
        ('operating_profit_loss', cast(o_operating_profit_loss AS VARCHAR), cast(e_operating_profit_loss AS VARCHAR), 'value'),
        ('profit_loss_on_ordinary_activities_before_tax', cast(o_profit_loss_on_ordinary_activities_before_tax AS VARCHAR), cast(e_profit_loss_on_ordinary_activities_before_tax AS VARCHAR), 'value'),
        ('tax_on_profit_or_loss_on_ordinary_activities', cast(o_tax_on_profit_or_loss_on_ordinary_activities AS VARCHAR), cast(e_tax_on_profit_or_loss_on_ordinary_activities AS VARCHAR), 'value'),
        ('profit_loss_for_period', cast(o_profit_loss_for_period AS VARCHAR), cast(e_profit_loss_for_period AS VARCHAR), 'value'),
        ('error', cast(o_error AS VARCHAR), cast(e_error AS VARCHAR), 'meta')
) t(col, o_val, e_val, kind)
WHERE has_oracle AND has_extract
  AND NOT soft_equal(o_val, e_val);

CREATE OR REPLACE TABLE summary AS
SELECT
    (SELECT count(*) FROM oracle_wide) AS oracle_rows,
    (SELECT count(*) FROM extract_wide) AS extract_rows,
    (SELECT count(*) FROM paired WHERE has_oracle AND has_extract) AS paired_rows,
    (SELECT count(*) FROM paired WHERE has_oracle AND NOT has_extract) AS oracle_only_rows,
    (SELECT count(*) FROM paired WHERE has_extract AND NOT has_oracle) AS extract_only_rows,
    (SELECT count(*) FROM cell_diffs WHERE kind = 'value') AS value_mismatches,
    (SELECT count(*) FROM cell_diffs WHERE kind = 'meta') AS meta_mismatches;
