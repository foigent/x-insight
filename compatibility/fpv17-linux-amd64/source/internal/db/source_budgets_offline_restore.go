package db

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func validateBudgetOfflineRestore(ctx context.Context, q workspaceQueryer) error {
	var valid bool
	err := q.QueryRowContext(ctx, `SELECT
 (SELECT count(*)=2 FROM pg_trigger WHERE tgenabled='A' AND NOT tgisinternal AND (tgrelid,tgname) IN (
 ('source_budget_offline_authorizations'::regclass,'source_budget_offline_authorizations_guard'),
 ('source_budget_offline_imports'::regclass,'source_budget_offline_imports_immutable')))
 AND NOT EXISTS(SELECT 1 FROM source_budget_collections c WHERE (c.transport='bounded_offline')<>EXISTS(SELECT 1 FROM source_budget_offline_authorizations a WHERE a.collection_id=c.id))
 AND NOT EXISTS(SELECT 1 FROM source_budget_offline_imports i JOIN source_budget_catalogs c ON c.id=i.catalog_id WHERE c.collection_id<>i.collection_id)
 AND NOT EXISTS(SELECT 1 FROM source_budget_collections c WHERE c.transport='bounded_offline' AND (c.cycle>1 OR (c.cycle>0)<>EXISTS(SELECT 1 FROM source_budget_offline_imports i WHERE i.collection_id=c.id)))`).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("restored offline budget authority or receipt guard is invalid")
	}
	type entry struct {
		id, hash string
		raw      []byte
	}
	var entries []entry
	rows, err := q.QueryContext(ctx, `SELECT collection_id::text,authorization_bytes,authorization_sha256 FROM source_budget_offline_authorizations ORDER BY collection_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.raw, &e.hash); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		var unchecked sourcefacts.BudgetOfflineAuthorization
		if json.Unmarshal(e.raw, &unchecked) != nil {
			return fmt.Errorf("restored offline budget authorization is invalid")
		}
		start, err := time.Parse(sourcefacts.TimeFormat, unchecked.NotBefore)
		if err != nil {
			return err
		}
		a, err := sourcefacts.ParseBudgetOfflineAuthorization(e.raw, start)
		if err != nil || a.CollectionID != e.id || a.Digest() != e.hash || sourcefacts.Hash(e.raw) != e.hash {
			return fmt.Errorf("restored offline budget authorization digest is invalid")
		}
		c, err := loadBudgetCollection(ctx, q, e.id, false)
		if err != nil {
			return err
		}
		if !sourcefacts.SameBudgetCollection(c, a.Collection()) {
			return fmt.Errorf("restored offline budget scope differs")
		}
	}
	type imported struct {
		id, catalog, hash string
		raw, auth         []byte
		count             int
		received          time.Time
	}
	var imports []imported
	rows, err = q.QueryContext(ctx, `SELECT i.collection_id::text,i.catalog_id::text,i.bundle_bytes,i.bundle_sha256,i.source_reading_count,i.received_at,a.authorization_bytes FROM source_budget_offline_imports i JOIN source_budget_offline_authorizations a USING(collection_id) ORDER BY i.collection_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var i imported
		if err = rows.Scan(&i.id, &i.catalog, &i.raw, &i.hash, &i.count, &i.received, &i.auth); err != nil {
			rows.Close()
			return err
		}
		imports = append(imports, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range imports {
		a, err := sourcefacts.ParseBudgetOfflineAuthorization(i.auth, i.received)
		if err != nil {
			return fmt.Errorf("restored budget import occurred outside its authorization")
		}
		b, err := sourcefacts.ValidateBudgetOfflineBundle(i.raw, a, i.received)
		if err != nil || len(b.Readings) != i.count || i.hash != a.BundleSHA256 {
			return fmt.Errorf("restored offline budget bundle is invalid")
		}
		var catalog []byte
		var count int
		if err = q.QueryRowContext(ctx, `SELECT catalog_bytes FROM source_budget_catalogs WHERE id=$1::uuid`, i.catalog).Scan(&catalog); err != nil {
			return err
		}
		actual, err := sourcefacts.DecodeBudgetCatalog(catalog)
		if err != nil {
			return err
		}
		expected, _ := sourcefacts.DecodeBudgetCatalog(b.Catalog)
		canonicalActual, _ := json.Marshal(actual)
		canonicalExpected, _ := json.Marshal(expected)
		if !bytes.Equal(canonicalActual, canonicalExpected) {
			return fmt.Errorf("restored offline catalog differs from bundle")
		}
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM source_budget_receipts WHERE collection_id=$1::uuid`, i.id).Scan(&count); err != nil {
			return err
		}
		if count != len(b.Readings) {
			return fmt.Errorf("restored offline receipt count differs")
		}
		for _, entry := range b.Readings {
			r, _ := sourcefacts.DecodeBudgetReading(entry.Reading)
			var valid bool
			err = q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM source_budget_receipts p JOIN source_budget_readings r ON r.id=p.source_reading_id WHERE p.collection_id=$1::uuid AND p.catalog_id=$2::uuid AND p.alerts_input_cursor=$3 AND r.source_system_id=$4::uuid AND r.reading_id=$5 AND r.reading_sha256=$6 AND p.received_at=$7)`, i.id, i.catalog, entry.AlertsInputCursor, a.SourceSystemID, r.ReadingID, r.SHA256, i.received).Scan(&valid)
			if err != nil {
				return err
			}
			if !valid {
				return fmt.Errorf("restored offline source reading differs from bundle")
			}
		}
	}
	return nil
}
