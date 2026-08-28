-- Soft-compare stream-read-xbrl wide rows (oracle_wide) to extract_wide.
-- compare_spec.kind:
--   must     — oracle non-empty ⇒ extract must soft-equal
--   observe  — both non-empty ⇒ must soft-equal (else informational)
--   skip     — never counted toward FAIL

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

CREATE OR REPLACE MACRO empty_val(v) AS (soft_value(v) = '');

CREATE OR REPLACE MACRO soft_equal(a, b) AS (
    empty_val(a) AND empty_val(b)
    OR soft_value(a) = soft_value(b)
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
    coalesce(o.source_file, e.source_file) AS source_file,
    o.period_start AS o_period_start,
    o.period_end AS o_period_end,
    e.period_start AS e_period_start,
    e.period_end AS e_period_end,
    (o.companies_house_registered_number IS NOT NULL
        OR o.period_start IS NOT NULL
        OR o.period_end IS NOT NULL
        OR o.company_id IS NOT NULL) AS has_oracle,
    (e.company_id IS NOT NULL
        OR e.period_start IS NOT NULL
        OR e.period_end IS NOT NULL) AS has_extract,
    o.company_id AS o_company_id,
    e.company_id AS e_company_id,
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
    e.profit_loss_for_period AS e_profit_loss_for_period
FROM oracle_wide o
FULL OUTER JOIN extract_wide e
    ON o.source_file = e.source_file
   AND o.period_start IS NOT DISTINCT FROM e.period_start
   AND o.period_end IS NOT DISTINCT FROM e.period_end;

CREATE OR REPLACE TABLE cell_all AS
SELECT
    source_file,
    coalesce(cast(o_period_start AS VARCHAR), cast(e_period_start AS VARCHAR), '') AS period_start,
    coalesce(cast(o_period_end AS VARCHAR), cast(e_period_end AS VARCHAR), '') AS period_end,
    col,
    o_val,
    e_val,
    s.kind,
    has_oracle,
    has_extract
FROM paired,
LATERAL (
    VALUES
        ('company_id', cast(o_company_id AS VARCHAR), cast(e_company_id AS VARCHAR)),
        ('balance_sheet_date', cast(o_balance_sheet_date AS VARCHAR), cast(e_balance_sheet_date AS VARCHAR)),
        ('companies_house_registered_number', cast(o_companies_house_registered_number AS VARCHAR), cast(e_companies_house_registered_number AS VARCHAR)),
        ('entity_current_legal_name', cast(o_entity_current_legal_name AS VARCHAR), cast(e_entity_current_legal_name AS VARCHAR)),
        ('company_dormant', cast(o_company_dormant AS VARCHAR), cast(e_company_dormant AS VARCHAR)),
        ('average_number_employees_during_period', cast(o_average_number_employees_during_period AS VARCHAR), cast(e_average_number_employees_during_period AS VARCHAR)),
        ('tangible_fixed_assets', cast(o_tangible_fixed_assets AS VARCHAR), cast(e_tangible_fixed_assets AS VARCHAR)),
        ('debtors', cast(o_debtors AS VARCHAR), cast(e_debtors AS VARCHAR)),
        ('cash_bank_in_hand', cast(o_cash_bank_in_hand AS VARCHAR), cast(e_cash_bank_in_hand AS VARCHAR)),
        ('current_assets', cast(o_current_assets AS VARCHAR), cast(e_current_assets AS VARCHAR)),
        ('creditors_due_within_one_year', cast(o_creditors_due_within_one_year AS VARCHAR), cast(e_creditors_due_within_one_year AS VARCHAR)),
        ('creditors_due_after_one_year', cast(o_creditors_due_after_one_year AS VARCHAR), cast(e_creditors_due_after_one_year AS VARCHAR)),
        ('net_current_assets_liabilities', cast(o_net_current_assets_liabilities AS VARCHAR), cast(e_net_current_assets_liabilities AS VARCHAR)),
        ('total_assets_less_current_liabilities', cast(o_total_assets_less_current_liabilities AS VARCHAR), cast(e_total_assets_less_current_liabilities AS VARCHAR)),
        ('net_assets_liabilities_including_pension_asset_liability', cast(o_net_assets_liabilities_including_pension_asset_liability AS VARCHAR), cast(e_net_assets_liabilities_including_pension_asset_liability AS VARCHAR)),
        ('called_up_share_capital', cast(o_called_up_share_capital AS VARCHAR), cast(e_called_up_share_capital AS VARCHAR)),
        ('profit_loss_account_reserve', cast(o_profit_loss_account_reserve AS VARCHAR), cast(e_profit_loss_account_reserve AS VARCHAR)),
        ('shareholder_funds', cast(o_shareholder_funds AS VARCHAR), cast(e_shareholder_funds AS VARCHAR)),
        ('turnover_gross_operating_revenue', cast(o_turnover_gross_operating_revenue AS VARCHAR), cast(e_turnover_gross_operating_revenue AS VARCHAR)),
        ('other_operating_income', cast(o_other_operating_income AS VARCHAR), cast(e_other_operating_income AS VARCHAR)),
        ('cost_sales', cast(o_cost_sales AS VARCHAR), cast(e_cost_sales AS VARCHAR)),
        ('gross_profit_loss', cast(o_gross_profit_loss AS VARCHAR), cast(e_gross_profit_loss AS VARCHAR)),
        ('administrative_expenses', cast(o_administrative_expenses AS VARCHAR), cast(e_administrative_expenses AS VARCHAR)),
        ('raw_materials_consumables', cast(o_raw_materials_consumables AS VARCHAR), cast(e_raw_materials_consumables AS VARCHAR)),
        ('staff_costs', cast(o_staff_costs AS VARCHAR), cast(e_staff_costs AS VARCHAR)),
        ('depreciation_other_amounts_written_off_tangible_intangible_fixed_assets', cast(o_depreciation_other_amounts_written_off_tangible_intangible_fixed_assets AS VARCHAR), cast(e_depreciation_other_amounts_written_off_tangible_intangible_fixed_assets AS VARCHAR)),
        ('other_operating_charges_format2', cast(o_other_operating_charges_format2 AS VARCHAR), cast(e_other_operating_charges_format2 AS VARCHAR)),
        ('operating_profit_loss', cast(o_operating_profit_loss AS VARCHAR), cast(e_operating_profit_loss AS VARCHAR)),
        ('profit_loss_on_ordinary_activities_before_tax', cast(o_profit_loss_on_ordinary_activities_before_tax AS VARCHAR), cast(e_profit_loss_on_ordinary_activities_before_tax AS VARCHAR)),
        ('tax_on_profit_or_loss_on_ordinary_activities', cast(o_tax_on_profit_or_loss_on_ordinary_activities AS VARCHAR), cast(e_tax_on_profit_or_loss_on_ordinary_activities AS VARCHAR)),
        ('profit_loss_for_period', cast(o_profit_loss_for_period AS VARCHAR), cast(e_profit_loss_for_period AS VARCHAR))
) t(col, o_val, e_val)
INNER JOIN compare_spec s ON s.column_name = t.col;

CREATE OR REPLACE TABLE cell_diffs AS
SELECT
    source_file,
    period_start,
    period_end,
    col,
    o_val,
    e_val,
    kind
FROM cell_all
WHERE has_oracle AND has_extract
  AND NOT soft_equal(o_val, e_val)
  AND (
      (kind = 'must' AND NOT empty_val(o_val))
      OR (kind = 'observe' AND NOT empty_val(o_val) AND NOT empty_val(e_val))
  );

CREATE OR REPLACE TABLE oracle_only_must AS
SELECT DISTINCT
    source_file,
    period_start,
    period_end
FROM cell_all
WHERE has_oracle AND NOT has_extract
  AND kind = 'must'
  AND NOT empty_val(o_val);

CREATE OR REPLACE TABLE per_file AS
SELECT
    f.source_file,
    (SELECT count(*) FROM oracle_wide o WHERE o.source_file = f.source_file) AS oracle_rows,
    (SELECT count(*) FROM extract_wide e WHERE e.source_file = f.source_file) AS extract_rows,
    (SELECT count(*) FROM paired p
     WHERE p.source_file = f.source_file AND p.has_oracle AND p.has_extract) AS paired_rows,
    (SELECT count(*) FROM paired p
     WHERE p.source_file = f.source_file AND p.has_oracle AND NOT p.has_extract) AS oracle_only_rows,
    (SELECT count(*) FROM paired p
     WHERE p.source_file = f.source_file AND p.has_extract AND NOT p.has_oracle) AS extract_only_rows,
    (SELECT count(*) FROM cell_diffs d
     WHERE d.source_file = f.source_file AND d.kind = 'must') AS must_mismatches,
    (SELECT count(*) FROM cell_diffs d
     WHERE d.source_file = f.source_file AND d.kind = 'observe') AS observe_mismatches,
    (SELECT count(*) FROM oracle_only_must m
     WHERE m.source_file = f.source_file) AS oracle_only_must_rows
FROM (SELECT DISTINCT source_file FROM file_map) f;

CREATE OR REPLACE TABLE summary AS
SELECT
    (SELECT count(*) FROM oracle_wide) AS oracle_rows,
    (SELECT count(*) FROM extract_wide) AS extract_rows,
    (SELECT count(*) FROM paired WHERE has_oracle AND has_extract) AS paired_rows,
    (SELECT count(*) FROM paired WHERE has_oracle AND NOT has_extract) AS oracle_only_rows,
    (SELECT count(*) FROM paired WHERE has_extract AND NOT has_oracle) AS extract_only_rows,
    (SELECT count(*) FROM cell_diffs WHERE kind = 'must') AS must_mismatches,
    (SELECT count(*) FROM cell_diffs WHERE kind = 'observe') AS observe_mismatches,
    (SELECT count(*) FROM oracle_only_must) AS oracle_only_must_rows,
    (SELECT count(*) FROM per_file) AS files;
