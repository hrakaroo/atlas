"""Launch specification tests; no launchd registration or live library access."""
import contextlib
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'app'))
sys.path.insert(0,str(ROOT/'scripts'))
import service


class GoPackagingTests(unittest.TestCase):
    def test_default_spec_uses_go_without_any_archived_backend(self):
        with tempfile.TemporaryDirectory() as folder:
            library=Path(folder)/'Atlas library'
            spec=service.launch_spec(library,47831,owner_name='Kai Martínez',timezone='UTC')
            self.assertEqual(spec['Label'],service.LABEL)
            self.assertEqual(spec['WorkingDirectory'],str(library.resolve()))
            self.assertIn('--go-service',spec['ProgramArguments'])
            self.assertNotIn('--python',spec['ProgramArguments'])
            self.assertNotIn('--server',spec['ProgramArguments'])
            self.assertIn(str(ROOT/'build/Atlas.app/Contents/Resources/bin/atlas-service'),spec['ProgramArguments'])
            self.assertFalse(library.exists())

    def test_go_spec_is_pure_and_preserves_unicode_paths_and_explicit_configuration(self):
        with tempfile.TemporaryDirectory() as folder:
            library=Path(folder)/'Atlas 東京 library'
            app=Path(folder)/'Preview Δ.app'
            spec=service.launch_spec(library,48901,'go',app,'Kai Martínez','Pacific/Auckland')
            arguments=spec['ProgramArguments']
            self.assertEqual(spec['Label'],service.LABEL)
            self.assertEqual(arguments,[str(app/'Contents/Library/LoginItems/AtlasService.app/Contents/MacOS/AtlasService'),
                '--service','--go-service',str(app/'Contents/Resources/bin/atlas-service'),
                '--owner-name','Kai Martínez','--timezone','Pacific/Auckland','--library',str(library.resolve()),'--port','48901'])
            self.assertNotIn('--python',arguments)
            self.assertEqual(spec['WorkingDirectory'],str(library.resolve()))
            self.assertNotEqual(spec['WorkingDirectory'],str(ROOT))
            self.assertFalse(library.exists())
            self.assertFalse(app.exists())
            for changes in ({'owner_name':''},{'timezone':'Not/AZone'},{'app':Path('relative.app')}):
                args=dict(library=library,port=48901,backend='go',app=app,owner_name='Kai Martínez',timezone='UTC')
                args.update(changes)
                with self.assertRaises(ValueError): service.launch_spec(**args)

    def test_unsupported_backend_rejects_before_any_writes(self):
        with tempfile.TemporaryDirectory() as folder,patch.object(service,'save_connection') as save,patch.object(service,'launchctl') as launch:
            with self.assertRaisesRegex(ValueError,'only the Go'):
                service.install(Path(folder)/'library',47831,backend='unsupported')
            save.assert_not_called();launch.assert_not_called()

    def test_invalid_go_install_config_fails_before_any_settings_write(self):
        with tempfile.TemporaryDirectory() as folder,patch.object(service,'save_connection') as save,patch.object(service,'launchctl') as launch:
            for changes in ({'port':0},{'app':Path('relative.app')},{'app':Path(folder)/'Missing.app'},{'owner_name':''},{'timezone':'Bad/Timezone'}):
                args=dict(library=Path(folder)/'library',port=47831,owner_name='Kai Martínez',timezone='UTC')
                args.update(changes)
                with self.assertRaises(ValueError): service.install(**args)
            save.assert_not_called();launch.assert_not_called()
            self.assertFalse((Path(folder)/'library').exists())

    def test_signed_go_install_configures_only_native_go_supervisor(self):
        with tempfile.TemporaryDirectory() as folder:
            base=Path(folder);app=base/'Atlas.app';library=base/'library';plist=base/'LaunchAgents/atlas.plist'
            helper=app/'Contents/Library/LoginItems/AtlasService.app/Contents/MacOS/AtlasService'
            native=app/'Contents/Resources/bin/atlas-service'
            helper.parent.mkdir(parents=True);helper.write_text('fixture')
            native.parent.mkdir(parents=True);native.write_text('fixture')
            from types import SimpleNamespace
            with patch.object(service,'PLIST',plist),patch.object(service,'launchctl',side_effect=lambda *args,**kwargs: SimpleNamespace(returncode=1 if args[0]=='print' else 0,stderr='')) as launch,patch.object(service,'save_connection',return_value={'url':'http://127.0.0.1:47831'}) as save,patch.object(service,'wait_ready',return_value={'pid':123}),patch.object(service.subprocess,'run') as signature,contextlib.redirect_stdout(io.StringIO()):
                service.install(library,47831,app=app,owner_name='Kai Martínez',timezone='UTC')
            import plistlib
            installed=plistlib.loads(plist.read_bytes())
            self.assertEqual(installed,service.launch_spec(library,47831,app=app,owner_name='Kai Martínez',timezone='UTC'))
            self.assertNotIn('--python',installed['ProgramArguments'])
            signature.assert_called_once_with(['/usr/bin/codesign','--verify','--deep','--strict',str(app)],check=True,capture_output=True)
            self.assertTrue(any(c.args[0]=='bootstrap' for c in launch.call_args_list))
            save.assert_called_once()

    def test_print_go_spec_action_never_calls_mutating_or_network_functions(self):
        with tempfile.TemporaryDirectory() as folder:
            argv=['service.py','print-launch-spec','--backend','go','--library',folder+'/library',
                  '--app',folder+'/Preview.app','--owner-name','Kai Martínez','--timezone','UTC']
            with patch.object(sys,'argv',argv),patch.object(service,'launchctl') as launch,patch.object(service,'save_connection') as save,patch.object(service,'request') as request,contextlib.redirect_stdout(io.StringIO()) as out:
                self.assertEqual(service.main(),0)
                self.assertIn('--go-service',json.loads(out.getvalue())['ProgramArguments'])
                launch.assert_not_called();save.assert_not_called();request.assert_not_called()
            self.assertFalse((Path(folder)/'library').exists())


class NoticeGenerationTests(unittest.TestCase):
    def tool(self):
        spec=importlib.util.spec_from_file_location('atlas_notice_generator',ROOT/'service/tools/notices.py')
        module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
        return module

    def test_notice_generation_copies_original_bytes_and_records_all_components(self):
        tool=self.tool()
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);dependency=root/'dependency';dependency.mkdir()
            compiler=root/'compiler';compiler.mkdir()
            license_bytes=b'Original copyright\r\nAll rights reserved.\n'
            (dependency/'LICENSE').write_bytes(license_bytes)
            (dependency/'NOTICE').write_bytes(b'Additional notice')
            (compiler/'LICENSE').write_bytes(b'Go license')
            (root/'go.mod').write_text('module synthetic/module\n')
            modules=json.dumps({'Path':'example.test/dependency','Version':'v1.0.0','Dir':str(dependency),'Sum':'h1:observed'}).encode()
            compiler_info=json.dumps({'GOROOT':str(compiler),'GOVERSION':'go1.26.0'}).encode()
            with patch.object(tool,'ROOT',root),patch.object(tool,'go',side_effect=[modules,compiler_info]):
                text,metadata=tool.generate()
            self.assertIn(license_bytes,text)
            self.assertIn(b'Additional notice',text)
            manifest=json.loads(metadata)
            self.assertEqual([c['module'] for c in manifest['components']],['Go toolchain','example.test/dependency'])
            self.assertEqual(manifest['components'][1]['notices'][0]['sha256'],hashlib.sha256(license_bytes).hexdigest())
            self.assertNotIn(str(root),text.decode())
            self.assertNotIn(str(root),metadata.decode())

    def test_missing_required_notice_fails_instead_of_omitting_component(self):
        tool=self.tool()
        with tempfile.TemporaryDirectory() as folder:
            with self.assertRaisesRegex(ValueError,'notice missing'):
                tool.notice_files(Path(folder))
            (Path(folder)/'PATENTS').write_text('Patent grant only')
            with self.assertRaisesRegex(ValueError,'license missing'):
                tool.notice_files(Path(folder))


if __name__=='__main__': unittest.main()
