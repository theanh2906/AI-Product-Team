import { Component, HostListener } from '@angular/core';
import { RouterOutlet } from '@angular/router';

@Component({
  selector: 'app-root',
  imports: [RouterOutlet],
  templateUrl: './app.html',
  styleUrl: './app.css'
})
export class App {
  @HostListener('document:contextmenu', ['$event'])
  blockDefaultContextMenu(event: MouseEvent): void {
    event.preventDefault();
  }
}
